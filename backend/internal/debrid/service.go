package debrid

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

const (
	resolveTimeout        = 20 * time.Minute
	providerDeleteTimeout = 30 * time.Second
)

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
	// torboxLimiter is the one shared rate limiter for every TorBox
	// interaction Vorn makes, across all three services that talk to it
	// (this one's debrid-resolve client, nzb.Service's usenet caching,
	// torrent.Service's indexer search) -- see TorBoxLimiter.
	torboxLimiter *Limiter
}

func NewService(st *store.Store) *Service {
	torboxLimiter := NewLimiter(torBoxRateLimit)
	svc := &Service{
		store: st,
		providers: map[string]Provider{
			"realdebrid": NewRealDebridClient(),
			"torbox":     NewTorBoxClient(torboxLimiter),
			"alldebrid":  NewAllDebridClient(),
			"premiumize": NewPremiumizeClient(),
			"debridlink": NewDebridLinkClient(),
		},
		torboxLimiter: torboxLimiter,
	}
	svc.onComplete = func(item *store.DebridItem) {
		if item.MediaItemID == nil {
			PromoteCompleted(st, item)
			return
		}
		mediaItem, err := AuthorizedMediaItem(st, item)
		if err != nil {
			log.Printf("debrid: checking whether %s is still authoritative for %s: %v", item.ID, *item.MediaItemID, err)
			return
		}
		if mediaItem == nil {
			log.Printf("debrid: %s is a stale/abandoned resolve, a later attempt already took over -- skipping promotion", item.ID)
			return
		}
		if mediaItem.Kind == "season" {
			PromoteSeasonPackToExistingItems(st, mediaItem, item)
			return
		}
		PromoteToExistingItem(st, mediaItem, item)
	}
	return svc
}

// AddLinkInput is AddLink's request shape. MediaItemID is set only when
// this resolve is fulfilling a specific on-demand-acquisition placeholder
// (see the acquisition package) -- nil for manual/admin-added links, which
// keep going through PromoteCompleted's filename-guessing promotion.
type AddLinkInput struct {
	AccountID   string
	SourceRef   string
	Name        string
	LibraryID   *string
	MediaItemID *string
}

// AddLink registers a magnet link or info-hash against an account and
// starts resolving it in the background.
func (svc *Service) AddLink(in AddLinkInput) (*store.DebridItem, error) {
	account, err := svc.store.GetDebridAccount(in.AccountID)
	if err != nil {
		return nil, err
	}
	if !account.Enabled {
		return nil, fmt.Errorf("debrid: account %s is disabled", in.AccountID)
	}
	if _, ok := svc.providers[account.Provider]; !ok {
		return nil, fmt.Errorf("debrid: unknown provider %q", account.Provider)
	}

	item, err := svc.store.CreateDebridItem(store.CreateDebridItemInput{
		LibraryID:   in.LibraryID,
		AccountID:   in.AccountID,
		SourceRef:   in.SourceRef,
		Name:        in.Name,
		MediaItemID: in.MediaItemID,
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

	result, err := provider.Resolve(ctx, account.APIKey, item.SourceRef)
	if err != nil {
		// Deliberately does NOT touch the media_item here even when
		// MediaItemID is set: this resolve may be one of several retry
		// candidates the acquisition package is trying in sequence, and
		// only it knows whether this was the last one -- it detects this
		// failure itself by polling this debrid_item's own Status (set by
		// FinishDebridItem below) via acquisition.Service.waitForOutcome.
		if ferr := svc.store.FinishDebridItem(item.ID, err); ferr != nil {
			log.Printf("debrid: finishing %s: %v", item.ID, ferr)
		}
		return
	}

	if result.ProviderRef != "" {
		if err := svc.store.SetDebridItemProviderRef(item.ID, result.ProviderRef); err != nil {
			log.Printf("debrid: recording provider ref for %s: %v", item.ID, err)
		}
	}

	for _, f := range result.Files {
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

// Remove deletes item from the provider's own account (best-effort -- logged
// and ignored on failure, since a stale/already-gone remote item shouldn't
// block removing Vorn's own record of it) before marking it removed locally.
func (svc *Service) Remove(id string) error {
	item, err := svc.store.GetDebridItem(id)
	if err != nil {
		return err
	}
	if item.ProviderRef != "" {
		if account, aerr := svc.store.GetDebridAccount(item.AccountID); aerr != nil {
			log.Printf("debrid: loading account to delete %s: %v", id, aerr)
		} else if provider, ok := svc.providers[account.Provider]; ok {
			ctx, cancel := context.WithTimeout(context.Background(), providerDeleteTimeout)
			if derr := provider.Delete(ctx, account.APIKey, item.ProviderRef); derr != nil {
				log.Printf("debrid: deleting %s (%s) from %s: %v", id, item.ProviderRef, account.Provider, derr)
			}
			cancel()
		}
	}
	return svc.store.RemoveDebridItem(id)
}

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

// TestAccount verifies a provider/apiKey pair by fetching that provider's
// account info, without requiring the account to be saved first.
func (svc *Service) TestAccount(ctx context.Context, provider, apiKey string) (*AccountInfo, error) {
	p, ok := svc.providers[provider]
	if !ok {
		return nil, fmt.Errorf("debrid: unknown provider %q", provider)
	}
	return p.AccountInfo(ctx, apiKey)
}

// TorBoxLimiter exposes the one shared rate limiter every TorBox
// interaction this process makes shares -- nzb.Service (usenet caching)
// and torrent.Service (indexer search) both take this same instance at
// construction, in httpapi.Server.reconfigure, rather than each building
// their own independent 300/min budget against the same account.
func (svc *Service) TorBoxLimiter() *Limiter {
	return svc.torboxLimiter
}
