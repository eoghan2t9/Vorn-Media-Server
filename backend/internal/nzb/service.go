package nzb

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

const (
	dialTimeout       = 30 * time.Second
	repairTimeout     = 30 * time.Minute
	progressMinPeriod = 500 * time.Millisecond
)

// Service downloads NZB releases: parsing the index, fetching segments
// from a configured Usenet server (with up to MaxConnections in parallel
// per file), reassembling them, optionally repairing with par2, and
// promoting the result into the library on completion.
type Service struct {
	store       *store.Store
	downloadDir string
	onComplete  func(*store.NZBDownload)
}

func NewService(st *store.Store, downloadDir string) (*Service, error) {
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		return nil, fmt.Errorf("nzb: creating download dir: %w", err)
	}
	svc := &Service{store: st, downloadDir: downloadDir}
	svc.onComplete = func(n *store.NZBDownload) { PromoteCompleted(st, n) }
	return svc, nil
}

// AddNZB parses a .nzb file's bytes and starts downloading it in the
// background against whichever configured Usenet server is enabled.
func (svc *Service) AddNZB(data []byte, libraryID *string) (*store.NZBDownload, error) {
	doc, err := Parse(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("nzb: parsing nzb file: %w", err)
	}
	if len(doc.Files) == 0 {
		return nil, fmt.Errorf("nzb: nzb file has no <file> entries")
	}

	name := doc.Title()
	if name == "" {
		name = SubjectFilename(doc.Files[0].Subject)
	}

	rec, err := svc.store.CreateNZBDownload(store.CreateNZBDownloadInput{
		LibraryID: libraryID,
		Name:      name,
		SavePath:  svc.downloadDir,
	})
	if err != nil {
		return nil, err
	}

	go svc.run(rec, doc)
	return rec, nil
}

func (svc *Service) run(rec *store.NZBDownload, doc *NZB) {
	server, err := svc.pickServer()
	if err != nil {
		svc.finish(rec, err)
		return
	}

	var total int64
	for _, f := range doc.Files {
		for _, seg := range f.Segments {
			total += seg.Bytes
		}
	}
	if err := svc.store.UpdateNZBProgress(rec.ID, total, 0, "downloading"); err != nil {
		log.Printf("nzb: setting total bytes for %s: %v", rec.ID, err)
	}

	outDir := filepath.Join(rec.SavePath, rec.Name)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		svc.finish(rec, err)
		return
	}

	progress := &progressReporter{store: svc.store, recID: rec.ID, total: total}
	var done atomic.Int64

	for _, f := range doc.Files {
		if err := svc.downloadFile(server, outDir, f, &done, progress); err != nil {
			svc.finish(rec, err)
			return
		}
	}
	progress.forceReport(done.Load(), "repairing")

	ctx, cancel := context.WithTimeout(context.Background(), repairTimeout)
	if err := repairWithPar2(ctx, outDir); err != nil {
		log.Printf("nzb: par2 repair for %s: %v", rec.ID, err)
	}
	cancel()

	svc.finish(rec, nil)
	if svc.onComplete != nil {
		fresh, err := svc.store.GetNZBDownload(rec.ID)
		if err != nil {
			log.Printf("nzb: reloading %s for completion callback: %v", rec.ID, err)
			return
		}
		svc.onComplete(fresh)
	}
}

// downloadFile fetches every segment of f, decoding and writing each one
// at its yEnc-declared offset so segments can complete out of order. The
// real filename only becomes known once the first segment's =ybegin
// header is decoded, so that segment is fetched up front (on the
// connection that then gets reused as one of the pool's workers) before
// the output file is opened.
func (svc *Service) downloadFile(server *store.UsenetServer, outDir string, f File, done *atomic.Int64, progress *progressReporter) error {
	if len(f.Segments) == 0 {
		return fmt.Errorf("nzb: file %q has no segments", f.Subject)
	}
	group := ""
	if len(f.Groups) > 0 {
		group = f.Groups[0]
	}

	first, err := svc.dialServer(server, group)
	if err != nil {
		return err
	}
	firstData, firstMeta, err := fetchAndDecode(first, f.Segments[0].MessageID)
	if err != nil {
		first.Close()
		return fmt.Errorf("nzb: fetching first segment of %q: %w", f.Subject, err)
	}

	name := firstMeta.Name
	if name == "" {
		name = SubjectFilename(f.Subject)
	}
	out, err := os.OpenFile(filepath.Join(outDir, name), os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		first.Close()
		return err
	}
	defer out.Close()

	if err := writeSegment(out, firstMeta, firstData); err != nil {
		first.Close()
		return err
	}
	done.Add(int64(len(firstData)))
	progress.report(done.Load())

	remaining := f.Segments[1:]
	segCh := make(chan Segment, len(remaining))
	for _, s := range remaining {
		segCh <- s
	}
	close(segCh)

	maxConns := server.MaxConnections
	if maxConns < 1 {
		maxConns = 1
	}

	var wg sync.WaitGroup
	var firstErr atomic.Pointer[error]
	storeErr := func(err error) { firstErr.CompareAndSwap(nil, &err) }

	worker := func(reuse *Conn) {
		defer wg.Done()
		c := reuse
		if c == nil {
			var err error
			c, err = svc.dialServer(server, group)
			if err != nil {
				storeErr(err)
				return
			}
		}
		defer c.Close()

		for seg := range segCh {
			if firstErr.Load() != nil {
				return
			}
			data, meta, err := fetchAndDecode(c, seg.MessageID)
			if err != nil {
				storeErr(fmt.Errorf("nzb: segment %d of %q: %w", seg.Number, name, err))
				return
			}
			if err := writeSegment(out, meta, data); err != nil {
				storeErr(err)
				return
			}
			done.Add(int64(len(data)))
			progress.report(done.Load())
		}
	}

	wg.Add(maxConns)
	go worker(first)
	for i := 1; i < maxConns; i++ {
		go worker(nil)
	}
	wg.Wait()

	if p := firstErr.Load(); p != nil {
		return *p
	}
	return nil
}

func fetchAndDecode(c *Conn, messageID string) ([]byte, meta, error) {
	raw, err := c.Body(messageID)
	if err != nil {
		return nil, meta{}, err
	}
	return decodeYenc(raw)
}

func writeSegment(out *os.File, m meta, data []byte) error {
	offset := m.PartBegin - 1
	if offset < 0 {
		offset = 0
	}
	_, err := out.WriteAt(data, offset)
	return err
}

func (svc *Service) dialServer(server *store.UsenetServer, group string) (*Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()

	c, err := Dial(ctx, server.Host, server.Port, server.UseTLS)
	if err != nil {
		return nil, err
	}
	if err := c.Authenticate(server.Username, server.Password); err != nil {
		c.Close()
		return nil, err
	}
	if group != "" {
		if err := c.Group(group); err != nil {
			c.Close()
			return nil, err
		}
	}
	return c, nil
}

// TestServer dials and authenticates against a Usenet server without
// requiring it to be saved first, so an admin can validate host/port/TLS/
// credentials from the add-server form before committing to them.
func (svc *Service) TestServer(host string, port int, useTLS bool, username, password string) error {
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()
	c, err := Dial(ctx, host, port, useTLS)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Authenticate(username, password)
}

func (svc *Service) pickServer() (*store.UsenetServer, error) {
	servers, err := svc.store.ListUsenetServers()
	if err != nil {
		return nil, err
	}
	for _, s := range servers {
		if s.Enabled {
			return s, nil
		}
	}
	return nil, fmt.Errorf("nzb: no enabled usenet server configured")
}

func (svc *Service) finish(rec *store.NZBDownload, err error) {
	if ferr := svc.store.FinishNZBDownload(rec.ID, err); ferr != nil {
		log.Printf("nzb: finishing %s: %v", rec.ID, ferr)
	}
}

// progressReporter throttles Postgres progress writes: yEnc segments are
// commonly a few hundred KB each, so a multi-GB download can have
// thousands of them, and writing on every single one would just add load
// without giving the admin UI any perceptibly finer-grained progress.
type progressReporter struct {
	store  *store.Store
	recID  string
	total  int64
	lastNs atomic.Int64
}

func (p *progressReporter) report(done int64) {
	now := time.Now().UnixNano()
	last := p.lastNs.Load()
	if now-last < int64(progressMinPeriod) {
		return
	}
	if !p.lastNs.CompareAndSwap(last, now) {
		return
	}
	if err := p.store.UpdateNZBProgress(p.recID, p.total, done, "downloading"); err != nil {
		log.Printf("nzb: updating progress for %s: %v", p.recID, err)
	}
}

func (p *progressReporter) forceReport(done int64, status string) {
	if err := p.store.UpdateNZBProgress(p.recID, p.total, done, status); err != nil {
		log.Printf("nzb: updating progress for %s: %v", p.recID, err)
	}
}

// List returns every non-removed NZB download.
func (svc *Service) List() ([]*store.NZBDownload, error) {
	return svc.store.ListNZBDownloads()
}

// Remove marks a download removed. If deleteFiles is set, downloaded data
// is deleted from disk too.
func (svc *Service) Remove(id string, deleteFiles bool) error {
	rec, err := svc.store.GetNZBDownload(id)
	if err != nil {
		return err
	}
	if deleteFiles && rec.Name != "" {
		if err := os.RemoveAll(filepath.Join(rec.SavePath, rec.Name)); err != nil {
			log.Printf("nzb: deleting files for %s: %v", id, err)
		}
	}
	return svc.store.RemoveNZBDownload(id)
}

func (svc *Service) AddServer(in store.UsenetServer) (*store.UsenetServer, error) {
	return svc.store.CreateUsenetServer(in)
}

func (svc *Service) ListServers() ([]*store.UsenetServer, error) {
	return svc.store.ListUsenetServers()
}

func (svc *Service) RemoveServer(id string) error {
	return svc.store.DeleteUsenetServer(id)
}
