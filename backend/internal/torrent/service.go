package torrent

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	lt "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

const progressPollInterval = 2 * time.Second

// Service manages the lifecycle of BitTorrent downloads: adding magnets or
// .torrent files, persisting progress into Postgres, and promoting
// completed downloads into the library.
type Service struct {
	store       *store.Store
	client      *lt.Client
	downloadDir string
	onComplete  func(*store.Torrent)

	mu     sync.Mutex
	active map[string]*lt.Torrent // info hash -> handle
}

func NewService(st *store.Store, downloadDir string, peerPort int) (*Service, error) {
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		return nil, fmt.Errorf("torrent: creating download dir: %w", err)
	}
	cl, err := newClient(downloadDir, peerPort)
	if err != nil {
		return nil, fmt.Errorf("torrent: creating client: %w", err)
	}
	svc := &Service{
		store:       st,
		client:      cl,
		downloadDir: downloadDir,
		active:      make(map[string]*lt.Torrent),
	}
	svc.resumeActive()
	return svc, nil
}

// OnComplete registers a callback invoked once a torrent finishes
// downloading, so an auto-add watcher can promote its files into the
// library. Must be called before any torrents are added.
func (svc *Service) OnComplete(fn func(*store.Torrent)) {
	svc.onComplete = fn
}

func (svc *Service) Close() {
	svc.client.Close()
}

// resumeActive re-adds every not-yet-terminal torrent from a previous run.
// Only magnet-added torrents can be resumed today, since raw .torrent bytes
// aren't persisted; a torrent-file-added download that was interrupted mid
// run will need to be re-added by hand.
func (svc *Service) resumeActive() {
	rows, err := svc.store.ListActiveTorrents()
	if err != nil {
		log.Printf("torrent: listing active torrents to resume: %v", err)
		return
	}
	for _, rec := range rows {
		if rec.MagnetURI == "" {
			log.Printf("torrent: cannot resume %s (%s): no magnet URI on record", rec.ID, rec.Name)
			continue
		}
		t, err := svc.client.AddMagnet(rec.MagnetURI)
		if err != nil {
			log.Printf("torrent: resuming %s: %v", rec.ID, err)
			continue
		}
		svc.mu.Lock()
		svc.active[rec.InfoHash] = t
		svc.mu.Unlock()
		go svc.watch(t, rec)
	}
}

// AddMagnet starts downloading a magnet link. libraryID is nil if the admin
// hasn't picked a destination library yet (it can be assigned later, before
// the download completes, to control auto-add promotion).
func (svc *Service) AddMagnet(magnetURI string, libraryID *string, sequential bool) (*store.Torrent, error) {
	t, err := svc.client.AddMagnet(magnetURI)
	if err != nil {
		return nil, fmt.Errorf("torrent: adding magnet: %w", err)
	}
	return svc.track(t, magnetURI, libraryID, sequential)
}

// AddTorrentFile starts downloading from raw .torrent file bytes.
func (svc *Service) AddTorrentFile(data []byte, libraryID *string, sequential bool) (*store.Torrent, error) {
	mi, err := metainfo.Load(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("torrent: parsing torrent file: %w", err)
	}
	t, err := svc.client.AddTorrent(mi)
	if err != nil {
		return nil, fmt.Errorf("torrent: adding torrent: %w", err)
	}
	return svc.track(t, "", libraryID, sequential)
}

func (svc *Service) track(t *lt.Torrent, magnetURI string, libraryID *string, sequential bool) (*store.Torrent, error) {
	rec, err := svc.store.CreateTorrent(store.CreateTorrentInput{
		LibraryID:  libraryID,
		InfoHash:   t.InfoHash().HexString(),
		Name:       t.Name(),
		MagnetURI:  magnetURI,
		SavePath:   svc.downloadDir,
		Sequential: sequential,
	})
	if err != nil {
		t.Drop()
		return nil, err
	}

	svc.mu.Lock()
	svc.active[rec.InfoHash] = t
	svc.mu.Unlock()

	go svc.watch(t, rec)
	return rec, nil
}

// watch waits for torrent metadata, applies the configured download
// strategy, then polls progress into Postgres until the torrent completes
// or is dropped, promoting it into the library exactly once.
func (svc *Service) watch(t *lt.Torrent, rec *store.Torrent) {
	select {
	case <-t.GotInfo():
	case <-t.Closed():
		return
	}

	if err := svc.store.UpdateTorrentProgress(rec.ID, t.Name(), t.Length(), t.BytesCompleted(), "downloading"); err != nil {
		log.Printf("torrent: updating %s after metadata: %v", rec.ID, err)
	}

	if rec.Sequential {
		downloadSequentially(t)
	} else {
		t.DownloadAll()
	}

	ticker := time.NewTicker(progressPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-t.Complete().On():
			svc.completeTorrent(t, rec)
			return
		case <-t.Closed():
			return
		case <-ticker.C:
			if err := svc.store.UpdateTorrentProgress(rec.ID, t.Name(), t.Length(), t.BytesCompleted(), "downloading"); err != nil {
				log.Printf("torrent: updating progress for %s: %v", rec.ID, err)
			}
		}
	}
}

func (svc *Service) completeTorrent(t *lt.Torrent, rec *store.Torrent) {
	if err := svc.store.UpdateTorrentProgress(rec.ID, t.Name(), t.Length(), t.BytesCompleted(), "completed"); err != nil {
		log.Printf("torrent: updating progress for %s at completion: %v", rec.ID, err)
	}
	if err := svc.store.FinishTorrent(rec.ID, nil); err != nil {
		log.Printf("torrent: finishing %s: %v", rec.ID, err)
	}
	if svc.onComplete != nil {
		fresh, err := svc.store.GetTorrent(rec.ID)
		if err != nil {
			log.Printf("torrent: reloading %s for completion callback: %v", rec.ID, err)
			return
		}
		svc.onComplete(fresh)
	}
}

// List returns every non-removed torrent.
func (svc *Service) List() ([]*store.Torrent, error) {
	return svc.store.ListTorrents()
}

// Remove drops a torrent from the client and marks it removed. If
// deleteFiles is set, downloaded data is deleted from disk too.
func (svc *Service) Remove(id string, deleteFiles bool) error {
	rec, err := svc.store.GetTorrent(id)
	if err != nil {
		return err
	}

	svc.mu.Lock()
	t, ok := svc.active[rec.InfoHash]
	if ok {
		delete(svc.active, rec.InfoHash)
	}
	svc.mu.Unlock()

	if ok {
		t.Drop()
	}
	if deleteFiles && rec.SavePath != "" && rec.Name != "" {
		if err := os.RemoveAll(filepath.Join(rec.SavePath, rec.Name)); err != nil {
			log.Printf("torrent: deleting files for %s: %v", id, err)
		}
	}
	return svc.store.RemoveTorrent(id)
}
