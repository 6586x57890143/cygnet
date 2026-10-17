package ledger

import "strings"

// maxAuditReason is Discord's limit on an audit log reason.
const maxAuditReason = 512

// Reason is what a bird sends as X-Audit-Log-Reason for e, so Discord's own
// audit log reads alike for every bird: "merlin · roles.jail · by
// 175928847299117063 · spam". By an ID, since Discord shows the header as
// plain text.
func Reason(e Entry) string {
	parts := []string{e.Bird, e.Action}
	if e.Actor.Kind == ActorMember {
		parts = append(parts, "by "+e.Actor.ID)
	} else {
		parts = append(parts, "automatic")
	}
	if e.Reason != "" {
		parts = append(parts, e.Reason)
	}
	return clip(strings.Join(parts, " · "), maxAuditReason)
}

// clip cuts s to n runes, marking the cut.
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-3]) + "..."
}
