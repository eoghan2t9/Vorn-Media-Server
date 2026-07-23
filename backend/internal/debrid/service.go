package debrid

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

const resolveTimeout = 20 * time.Minute

// Service resolves magnet links / info-hashes against a user's configured
// debrid provider accounts (Real-Debrid, TorBox), persisting the resulting
// direct stream URLs and promoting them into the library on completion.
// Unlike the torrent/NZB services, there is no local download step: a
// resolve either produces stream URLs or fails, so no on-disk state needs
// managing on removal.
type Service struct {
	store      *store.Store
	providers  map[string]Provider
	onComplete func(*store.DebridItem)
}

func NewService(st *store.Store) *Service {
	svc := &Service{
		store: st,
		providers: map[string]Provider{
			"realdebrid": NewRealDebridClient(),
			"torbox":     NewTorBoxClient(),
		},
	}
	svc.onComplete = func(item *store.DebridItem) { PromoteCompleted(st, item) }
	return svc
}

// AddLink registers a magnet link or info-hash against accountID and starts
// resolving it in the background.
func (svc *Service) AddLink(accountID, sourceRef, name string, libraryID *string) (*store.DebridItem, error) {
	account, err := svc.store.GetDebridAccount(accountID)
	if err != nil {
		return nil, err
	}
	if !account.Enabled {
		return nil, fmt.Errorf("debrid: account %s is disabled", accountID)
	}
	if _, ok := svc.providers[account.Provider]; !ok {
		return nil, fmt.Errorf("debrid: unknown provider %q", account.Provider)
	}

	item, err := svc.store.CreateDebridItem(store.CreateDebridItemInput{
		LibraryID: libraryID,
		AccountID: accountID,
		SourceRef: sourceRef,
		Name:      name,
	})
	if err != nil {
		return nil, err
	}

	go svc.run(item, account)
	return item, nil
}

func (svc *Service) run(item *store.DebridItem, account *store.DebridAccount) {
	provider := svc.providers[account.Provider]

	ctx, cancel := context.WithTimeout(context.Background(), resolveTimeout)
	defer cancel()

	files, err := provider.Resolve(ctx, account.APIKey, item.SourceRef)
	if err != nil {
		if ferr := svc.store.FinishDebridItem(item.ID, err); ferr != nil {
			log.Printf("debrid: finishing %s: %v", item.ID, ferr)
		}
		return
	}

	for _, f := range files {
		if _, err := svc.store.AddDebridFile(item.ID, f.Name, f.SizeBytes, f.StreamURL); err != nil {
			log.Printf("debrid: saving resolved file for %s: %v", item.ID, err)
		}
	}

	if err := svc.store.FinishDebridItem(item.ID, nil); err != nil {
		log.Printf("debrid: finishing %s: %v", item.ID, err)
		return
	}

	if svc.onComplete == nil {
		return
	}
	fresh, err := svc.store.GetDebridItem(item.ID)
	if err != nil {
		log.Printf("debrid: reloading %s for completion callback: %v", item.ID, err)
		return
	}
	svc.onComplete(fresh)
}

func (svc *Service) List() ([]*store.DebridItem, error) { return svc.store.ListDebridItems() }

func (svc *Service) ListFiles(itemID string) ([]*store.DebridFile, error) {
	return svc.store.ListDebridFiles(itemID)
}

func (svc *Service) Remove(id string) error { return svc.store.RemoveDebridItem(id) }

func (svc *Service) AddAccount(provider, apiKey string) (*store.DebridAccount, error) {
	if _, ok := svc.providers[provider]; !ok {
		return nil, fmt.Errorf("debrid: unknown provider %q", provider)
	}
	return svc.store.CreateDebridAccount(provider, apiKey)
}

func (svc *Service) ListAccounts() ([]*store.DebridAccount, error) {
	return svc.store.ListDebridAccounts()
}

func (svc *Service) RemoveAccount(id string) error { return svc.store.DeleteDebridAccount(id) }
