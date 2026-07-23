package torrent

import (
	"io"

	lt "github.com/anacrolix/torrent"
)

// downloadSequentially forces in-order, piece-by-piece downloading of every
// file in a torrent instead of the client's default rarest-first strategy.
// It works by driving a Reader across each file from start to end: reading
// (and discarding) sequentially is what causes anacrolix/torrent to raise
// the priority of the piece currently being read plus a readahead window,
// which is exactly the access pattern a streaming player needs.
func downloadSequentially(t *lt.Torrent) {
	for _, f := range t.Files() {
		go drainInOrder(f)
	}
}

func drainInOrder(f *lt.File) {
	r := f.NewReader()
	defer r.Close()
	r.SetReadahead(f.Length())
	io.Copy(io.Discard, r)
}
