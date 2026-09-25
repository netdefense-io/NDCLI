package mcp

import (
	"fmt"
	"strings"
	"time"

	"github.com/netdefense-io/NDCLI/internal/models"
	"github.com/netdefense-io/NDCLI/internal/service"
)

// Device-timezone echo on the scheduling tools.
//
// A scheduled run has three candidate timezones — the user's, the device's
// and UTC — and silently picking one of them on the control plane is the
// failure mode this guards against. So this shows the device's, it never
// adopts it: the instant sent to NDManager is the one resolveRunScheduledAt
// produced from what the caller passed, unchanged by anything here. The echo
// only tells the model what the same instant reads as on the box it is about
// to act on, and warns when that differs from what was asked for.
//
// It applies only when the target is exactly one device. An OU or org-wide
// run can span devices in several timezones, and naming one of them as "the"
// device timezone would be the same confident wrong answer in a new costume.

// singleDeviceTarget returns the one device a run targets, or "" when the
// target is an OU, the whole org, several devices, or nothing.
func singleDeviceTarget(in *runInput) string {
	if in == nil || in.Org || len(in.OUs) > 0 || len(in.Devices) != 1 {
		return ""
	}
	return strings.TrimSpace(in.Devices[0])
}

// deviceFactLocation turns a reported timezone fact into a *time.Location.
//
// The IANA name is preferred over the reported offset because the offset was
// measured when the agent collected it, and a run scheduled across a DST
// boundary reads differently on the device than that stale number says. The
// fixed-zone fallback covers a name this host has no tzdata entry for.
func deviceFactLocation(tz models.DeviceTimezoneFact) *time.Location {
	if loc, err := time.LoadLocation(tz.Name); err == nil {
		return loc
	}
	label := tz.Abbrev
	if label == "" {
		label = tz.Name
	}
	return time.FixedZone(label, tz.UTCOffsetSec)
}

// addDeviceTimezoneEcho adds `device_timezone`, `scheduled_at_device_local`
// and, on a mismatch, `warning` to a schedule echo. It is best-effort: an
// unreachable device, an agent that reports no facts, or a multi-device
// target all leave the echo exactly as it was, because a scheduling call must
// not fail over an informational field.
func (s *Server) addDeviceTimezoneEcho(org string, in *runInput, resolved *service.ScheduledAt, echo map[string]interface{}) map[string]interface{} {
	if echo == nil || resolved == nil {
		return echo
	}
	name := singleDeviceTarget(in)
	if name == "" {
		return echo
	}
	ctx, cancel := contextWithTimeout()
	defer cancel()
	device, err := s.svc.DeviceGet(ctx, org, name)
	if err != nil || device == nil {
		return echo
	}
	tz, ok := device.TimezoneFact()
	if !ok {
		return echo
	}
	deviceLocal := resolved.UTC.In(deviceFactLocation(tz))
	echo["device_timezone"] = tz.Name
	echo["scheduled_at_device_local"] = deviceLocal.Format(time.RFC3339)

	// The mismatch worth warning about is a different wall clock, not a
	// different spelling: "America/Sao_Paulo" and an explicit -03:00 name the
	// same reading of the same instant, and warning about those would train
	// the caller to ignore the field.
	_, requestedOffset := resolved.Zoned.Zone()
	_, deviceOffset := deviceLocal.Zone()
	if requestedOffset != deviceOffset {
		echo["warning"] = fmt.Sprintf(
			"Timezone mismatch: this instant was resolved as %s (%s) but device %q is in %s, where it lands at %s. "+
				"NDCLI does not substitute the device's timezone — confirm with the user which one they meant before scheduling.",
			resolved.RFC3339Zoned(), resolved.Zone, name, tz.Display(), deviceLocal.Format(time.RFC3339))
	}
	return echo
}
