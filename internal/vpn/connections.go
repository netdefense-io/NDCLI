package vpn

import (
	"sort"
	"strings"

	"github.com/netdefense-io/NDCLI/internal/models"
)

// ComputeEffectiveConnections computes all effective VPN connections from members, links, and network config.
func ComputeEffectiveConnections(
	network *models.VpnNetwork,
	members []models.VpnMember,
	links []models.VpnLink,
) []models.EffectiveConnection {
	// Separate members by role
	var hubs, spokes []models.VpnMember
	for _, m := range members {
		if strings.EqualFold(m.Role, "HUB") {
			hubs = append(hubs, m)
		} else {
			spokes = append(spokes, m)
		}
	}

	// Index links by canonical key
	type linkEntry struct {
		link     models.VpnLink
		consumed bool
	}
	linkMap := make(map[string]*linkEntry)
	for _, l := range links {
		key := canonicalKey(l.DeviceAName, l.DeviceBName)
		linkMap[key] = &linkEntry{link: l}
	}

	var connections []models.EffectiveConnection

	// Hub-spoke pairs (always implicit)
	for _, hub := range hubs {
		for _, spoke := range spokes {
			key := canonicalKey(hub.DeviceName, spoke.DeviceName)
			conn := models.EffectiveConnection{
				DeviceA:    hub.DeviceName,
				DeviceB:    spoke.DeviceName,
				RoleA:      "HUB",
				RoleB:      "SPOKE",
				PairType:   "hub-spoke",
				Source:      "implicit",
				Active:     true,
				VpnNetwork: network.Name,
			}

			if entry, ok := linkMap[key]; ok {
				conn.HasOverride = true
				conn.Active = entry.link.Enabled
				conn.HasPSK = entry.link.HasPSK
				entry.consumed = true
			}

			connections = append(connections, conn)
		}
	}

	// Hub-hub pairs (implicit only if auto_connect_hubs)
	if network.AutoConnectHubs {
		for i := 0; i < len(hubs); i++ {
			for j := i + 1; j < len(hubs); j++ {
				key := canonicalKey(hubs[i].DeviceName, hubs[j].DeviceName)
				conn := models.EffectiveConnection{
					DeviceA:    hubs[i].DeviceName,
					DeviceB:    hubs[j].DeviceName,
					RoleA:      "HUB",
					RoleB:      "HUB",
					PairType:   "hub-hub",
					Source:      "implicit",
					Active:     true,
					VpnNetwork: network.Name,
				}

				if entry, ok := linkMap[key]; ok {
					conn.HasOverride = true
					conn.Active = entry.link.Enabled
					conn.HasPSK = entry.link.HasPSK
					entry.consumed = true
				}

				connections = append(connections, conn)
			}
		}
	}

	// Remaining unconsumed links are explicit
	memberRoles := make(map[string]string)
	for _, m := range members {
		memberRoles[m.DeviceName] = strings.ToUpper(m.Role)
	}

	for _, entry := range linkMap {
		if entry.consumed {
			continue
		}
		l := entry.link
		roleA := memberRoles[l.DeviceAName]
		roleB := memberRoles[l.DeviceBName]
		pairType := classifyPairType(roleA, roleB)

		connections = append(connections, models.EffectiveConnection{
			DeviceA:    l.DeviceAName,
			DeviceB:    l.DeviceBName,
			RoleA:      roleA,
			RoleB:      roleB,
			PairType:   pairType,
			Source:      "explicit",
			Active:     l.Enabled,
			HasPSK:     l.HasPSK,
			VpnNetwork: l.VpnNetwork,
		})
	}

	// Sort: hub-hub first, then hub-spoke, then spoke-spoke; alphabetical within groups
	sort.Slice(connections, func(i, j int) bool {
		oi, oj := pairTypeOrder(connections[i].PairType), pairTypeOrder(connections[j].PairType)
		if oi != oj {
			return oi < oj
		}
		if connections[i].DeviceA != connections[j].DeviceA {
			return connections[i].DeviceA < connections[j].DeviceA
		}
		return connections[i].DeviceB < connections[j].DeviceB
	})

	return connections
}

// ClassifyPair determines pair type and source for two member roles.
func ClassifyPair(roleA, roleB string, autoConnectHubs bool) (pairType, source string) {
	a := strings.ToUpper(roleA)
	b := strings.ToUpper(roleB)
	pairType = classifyPairType(a, b)

	switch pairType {
	case "hub-spoke":
		source = "implicit"
	case "hub-hub":
		if autoConnectHubs {
			source = "implicit"
		} else {
			source = "explicit"
		}
	default:
		source = "explicit"
	}
	return
}

// FilterByDevice returns connections involving the given device.
func FilterByDevice(connections []models.EffectiveConnection, device string) []models.EffectiveConnection {
	var filtered []models.EffectiveConnection
	for _, c := range connections {
		if strings.EqualFold(c.DeviceA, device) || strings.EqualFold(c.DeviceB, device) {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

func canonicalKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "|" + b
}

func classifyPairType(roleA, roleB string) string {
	if roleA == "HUB" && roleB == "HUB" {
		return "hub-hub"
	}
	if (roleA == "HUB" && roleB == "SPOKE") || (roleA == "SPOKE" && roleB == "HUB") {
		return "hub-spoke"
	}
	return "spoke-spoke"
}

func pairTypeOrder(pt string) int {
	switch pt {
	case "hub-hub":
		return 0
	case "hub-spoke":
		return 1
	case "spoke-spoke":
		return 2
	default:
		return 3
	}
}
