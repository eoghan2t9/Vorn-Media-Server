package nzb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearchIndexerByIMDb_Movie(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("t") != "movie" || q.Get("imdbid") != "0137523" || q.Get("apikey") != "test-key" {
			t.Errorf("unexpected query: %s", r.URL.RawQuery)
		}
		if q.Has("season") || q.Has("ep") {
			t.Errorf("movie search shouldn't send season/ep: %s", r.URL.RawQuery)
		}
		w.Write([]byte(`<?xml version="1.0"?>
<rss><channel>
<item>
  <title>Fight Club 1999 1080p BluRay-GROUP</title>
  <link>http://example.test/dl/1</link>
  <pubDate>Mon, 02 Jan 2006 15:04:05 +0000</pubDate>
  <newznab:attr name="size" value="1500000000"/>
</item>
</channel></rss>`))
	}))
	defer srv.Close()

	results, err := SearchIndexerByIMDb(context.Background(), "TestIndexer", srv.URL, "test-key", "tt0137523", 0, 0)
	if err != nil {
		t.Fatalf("SearchIndexerByIMDb: %v", err)
	}
	if len(results) != 1 || results[0].Title != "Fight Club 1999 1080p BluRay-GROUP" || results[0].SizeBytes != 1500000000 {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestSearchIndexerByIMDb_Episode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("t") != "tvsearch" || q.Get("imdbid") != "9288030" || q.Get("season") != "2" || q.Get("ep") != "1" {
			t.Errorf("unexpected query: %s", r.URL.RawQuery)
		}
		w.Write([]byte(`<?xml version="1.0"?><rss><channel></channel></rss>`))
	}))
	defer srv.Close()

	results, err := SearchIndexerByIMDb(context.Background(), "TestIndexer", srv.URL, "test-key", "tt9288030", 2, 1)
	if err != nil {
		t.Fatalf("SearchIndexerByIMDb: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestSearchIndexerByIMDb_SeasonPackOmitsEp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("t") != "tvsearch" || q.Get("season") != "3" || q.Has("ep") {
			t.Errorf("expected season-only tvsearch, got: %s", r.URL.RawQuery)
		}
		w.Write([]byte(`<?xml version="1.0"?><rss><channel></channel></rss>`))
	}))
	defer srv.Close()

	if _, err := SearchIndexerByIMDb(context.Background(), "TestIndexer", srv.URL, "test-key", "tt9288030", 3, 0); err != nil {
		t.Fatalf("SearchIndexerByIMDb: %v", err)
	}
}
