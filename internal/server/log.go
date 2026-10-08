import (
	"fmt"
	"sync"
)

type Log struct {
	mu sync.Mutex
	records [] Record
}

func NewLog() *Log {
	return &Log{}
}

func (c *Log) Append(record Record) (unit64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	record.Offset = unit64(len(c.records))
	c.records = append(c.records, record)
	return record.Offset, nil
}

func (c *Log) Read(offset unit64) (Record,error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if offset >= unit64(len(c.records)) {
		return Record{}, ErrOffsetNotFound
	}
	return c.records[offset],nil
}

type Record struct {
	Value []byte `json:"value"`
	Offset unit64 `json:"offset"`
}
