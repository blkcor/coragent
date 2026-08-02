package jobs

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// DecodeRecords is the persistence restore path that constructs JobRecord
// values from durable JSON lines.
func DecodeRecords(reader io.Reader) ([]JobRecord, error) {
	scanner := bufio.NewScanner(reader)
	var records []JobRecord
	for scanner.Scan() {
		var record JobRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, fmt.Errorf("decode job record: %w", err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan job records: %w", err)
	}
	return records, nil
}
