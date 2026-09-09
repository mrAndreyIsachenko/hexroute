package notification

import (
	"encoding/json"
	"os"
	"time"
)

// maxRememberedDeliveries bounds what is carried across a restart.
//
// Only deliveries whose identity outlives the process are kept, and those are
// per policy generation — a few a month. The bound exists so a file that has
// been growing since the machine was built still costs one small read at start.
const maxRememberedDeliveries = 64

const deliveryRecordSchema = "hexroute.notification-deliveries.v1"

// DeliveryRecord is where deliveries that outlive the process are remembered.
//
// It is an interface so a caller that has nowhere to write — a test, a
// one-shot command — can pass nothing and get the old behaviour, which is
// correct for a process that will not be restarted into the same situation.
type DeliveryRecord interface {
	Load() []rememberedDelivery
	Save([]rememberedDelivery)
}

// rememberedDelivery is one delivery, in the form the file holds it.
type rememberedDelivery struct {
	IncidentID string    `json:"incident_id"`
	Generation uint64    `json:"generation"`
	Status     string    `json:"status"`
	Template   Template  `json:"template"`
	At         time.Time `json:"at"`
}

// DeliveryRecordAt remembers deliveries in one file.
func DeliveryRecordAt(path string) DeliveryRecord {
	if path == "" {
		return nil
	}
	return fileDeliveryRecord{path: path}
}

type fileDeliveryRecord struct{ path string }

type deliveryRecordFile struct {
	Schema     string               `json:"schema"`
	Deliveries []rememberedDelivery `json:"deliveries"`
}

// Load returns what was remembered, and nothing at all when it cannot be read.
//
// A record that cannot be proved is not a reason to stop announcing. The worst
// this costs is one repeated announcement; refusing to announce because a
// bookkeeping file is damaged would lose the thing the announcement exists for.
func (record fileDeliveryRecord) Load() []rememberedDelivery {
	raw, err := os.ReadFile(record.path)
	if err != nil {
		return nil
	}
	var file deliveryRecordFile
	if json.Unmarshal(raw, &file) != nil || file.Schema != deliveryRecordSchema {
		return nil
	}
	if len(file.Deliveries) > maxRememberedDeliveries {
		file.Deliveries = file.Deliveries[len(file.Deliveries)-maxRememberedDeliveries:]
	}
	return file.Deliveries
}

// Save writes the record, or leaves the previous one in place.
//
// The rename is what makes the file either wholly the old record or wholly the
// new one. A half-written record read at the next start would be discarded by
// Load, which costs a repeated announcement rather than a wrong silence.
func (record fileDeliveryRecord) Save(deliveries []rememberedDelivery) {
	if len(deliveries) > maxRememberedDeliveries {
		deliveries = deliveries[len(deliveries)-maxRememberedDeliveries:]
	}
	encoded, err := json.Marshal(deliveryRecordFile{
		Schema: deliveryRecordSchema, Deliveries: deliveries,
	})
	if err != nil {
		return
	}
	temporary := record.path + ".partial"
	if os.WriteFile(temporary, encoded, 0o600) != nil {
		return
	}
	_ = os.Rename(temporary, record.path)
}
