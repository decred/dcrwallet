package summaries

import (
	"time"
)

type SummaryResolution uint8

const (
	SummaryResolutionBlock SummaryResolution = iota
	SummaryResolutionHour
	SummaryResolutionDay
	SummaryResolutionMonth
	SummaryResolutionYear
	SummaryResolutionAll
)

type SummaryName string
type SeriesID uint8

type SummaryResultDataPoint struct {
	IntValue int64
	FloatValue float64
}
type SummaryResult map[SeriesID]*SummaryResultDataPoint

type SummaryResultFunc func (time.Time, SummaryResult) (bool, error)

type Summary interface {
	Calculate (beginHeight, endHeight int32, resolution SummaryResolution, f SummaryResultFunc) error
}

var epochTime = time.Date(0, 0, 0, 0, 0, 0, 0, time.Local)

// referenceTime returns the reference time a block belongs to, given a
// resolution for summaries.
//
// The reference time is the time of the start of a summary bucket/grouping. The
// given resolutions are mapped to the following times:
// SummaryResolutionBlock: The block time itself
// SummaryResolutionHour: The hour the block was mined (minutes/seconds are
// zeroed)
// SummaryResolutionDay: The day the block was mined (hour is zeroed)
// SummaryResolutionMonth: The month the block was mined (day is zeroed)
// SummaryResolutionYear: The year the block was mined (month is zeroed)
// SummaryResolutionAll: The zero (epoch) time is returned
func referenceTime(blockTime time.Time, resolution SummaryResolution) time.Time {
	switch resolution {
	case SummaryResolutionBlock: return blockTime;
	case SummaryResolutionHour: return blockTime.Truncate(time.Hour)
	case SummaryResolutionDay: return time.Date(blockTime.Year(),
		blockTime.Month(), blockTime.Day(), 0, 0, 0, 0, blockTime.Location())
	case SummaryResolutionMonth: return time.Date(blockTime.Year(),
		blockTime.Month(), 0, 0, 0, 0, 0, blockTime.Location())
	case SummaryResolutionYear: return time.Date(blockTime.Year(),
		0, 0, 0, 0, 0, 0, blockTime.Location())
	case SummaryResolutionAll: return epochTime
	default:
		panic("Unimplemented resolution in referenceTime(). Please fix this.")
	}
}
