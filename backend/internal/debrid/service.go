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
	store     *store.Store
	providers map[string]Provider
	// onComplete returns whether item's files actually ended up being used
	// (promoted into a media item, or a manual admin add) -- run() deletes
	// item from the provider when it returns false, since that means
	// nothing will ever use the quota/storage it's holding.
	onComplete func(*store.DebridItem) bool
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
	svc.onComplete = func(item *store.DebridItem) bool {
		if item.MediaItemID == nil {
			// Manual/admin-added link -- never auto-delete from the
			// provider even if filename-guess promotion finds nothing to
			// promote, since the admin explicitly asked for this one.
			PromoteCompleted(st, item)
			return true
		}
		mediaItem, err := st.GetMediaItem(*item.MediaItemID)
		if err != nil {
			log.Printf("debrid: loading media item for %s: %v", item.ID, err)
			return false
		}
		if mediaItem.Kind == "season" {
			return PromoteSeasonPackToExistingItems(st, mediaItem, item)
		}
		return PromoteToExistingItem(st, mediaItem, item)
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
// starts resolving it in the background. ctx bounds the resolve itself (on
// top of the internal resolveTimeout) -- acquisition.Service passes a
// context it cancels once it stops caring about this attempt (a rival
// candidate already won, or it's giving up), so a losing racer stops within
// one HTTP round-trip instead of running to completion pointlessly. Callers
// with no such notion of "giving up" (manual/admin adds) pass
// context.Background().
func (svc *Service) AddLink(ctx context.Context, in AddLinkInput) (*store.DebridItem, error) {
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

	go svc.run(ctx, item, account)
	return item, nil
}

func (svc *Service) run(ctx context.Context, item *store.DebridItem, account *store.DebridAccount) {
	provider := svc.providers[account.Provider]

	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()

	result, err := provider.Resolve(ctx, account.APIKey, item.SourceRef)
	if err != nil {
		// Deliberately does NOT touch the media_item here even when
		// MediaItemID is set: this resolve may be one of several candidates
		// acquisition.Service is racing concurrently, and only it knows
		// whether every one of them has now failed -- it detects this
		// failure itself by polling this debrid_item's own Status (set by
		// FinishDebridItem below).
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
	if used := svc.onComplete(fresh); !used {
		svc.deleteFromProvider(fresh.ID, fresh.ProviderRef, provider, account)
	}
}

// deleteFromProvider is Remove's cleanup step, factored out so run() can
// reuse it the moment a resolve turns out not to have been used (lost a
// race, failed content verification, no video file) instead of leaking that
// quota/storage until someone happens to remove the item manually later.
func (svc *Service) deleteFromProvider(debridItemID, providerRef string, provider Provider, account *store.DebridAccount) {
	if providerRef == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), providerDeleteTimeout)
	defer cancel()
	if err := provider.Delete(ctx, account.APIKey, providerRef); err != nil {
		log.Printf("debrid: deleting %s (%s) from %s: %v", debridItemID, providerRef, account.Provider, err)
	}
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
			svc.deleteFromProvider(item.ID, item.ProviderRef, provider, account)
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
