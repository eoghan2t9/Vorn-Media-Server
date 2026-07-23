package scanner

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

// QueuedFile is what producers (the real walker or the synthetic generator)
// push into DragonflyDB, and what the flusher pops back off to write into
// Postgres. Kept separate from DiscoveredFile so the synthetic generator
// doesn't need to touch disk to produce one.
type QueuedFile struct {
	Path       string    `json:"path"`
	SizeBytes  int64     `json:"sizeBytes"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

// Queue stages discovered files in DragonflyDB (Redis-protocol compatible)
// ahead of a batched flush into Postgres, decoupling scan throughput from
// database write throughput.
type Queue struct {
	rdb *redis.Client
}

func NewQueue(addr string) *Queue {
	return &Queue{rdb: redis.NewClient(&redis.Options{Addr: addr})}
}

func (q *Queue) Ping(ctx context.Context) error {
	return q.rdb.Ping(ctx).Err()
}

func (q *Queue) Close() error {
	return q.rdb.Close()
}

func queueKey(jobID string) string {
	return "vorn:scan:" + jobID + ":queue"
}

func (q *Queue) Push(ctx context.Context, jobID string, f QueuedFile) error {
	raw, err := json.Marshal(f)
	if err != nil {
		return err
	}
	return q.rdb.RPush(ctx, queueKey(jobID), raw).Err()
}

// PopBatch removes and returns up to n queued files. It returns fewer than n
// (possibly zero) if that's all that's currently available.
func (q *Queue) PopBatch(ctx context.Context, jobID string, n int64) ([]QueuedFile, error) {
	raws, err := q.rdb.LPopCount(ctx, queueKey(jobID), int(n)).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]QueuedFile, 0, len(raws))
	for _, raw := range raws {
		var f QueuedFile
		if err := json.Unmarshal([]byte(raw), &f); err != nil {
			continue
		}
		out = append(out, f)
	}
	return out, nil
}

func (q *Queue) Len(ctx context.Context, jobID string) (int64, error) {
	return q.rdb.LLen(ctx, queueKey(jobID)).Result()
}

func (q *Queue) Delete(ctx context.Context, jobID string) error {
	return q.rdb.Del(ctx, queueKey(jobID)).Err()
}
