package platform

import (
	"fmt"
	"hash/fnv"
	"strconv"

	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/prefs"
)

// SavePreferences applies the caller's own preferences and records email opt-in
// changes before mutation. Recipient addresses remain operator-owned configuration.
func (p *Platform) SavePreferences(who auth.Principal, value prefs.Preferences, personal ...prefs.Personal) (prefs.Preferences, error) {
	if len(personal) > 1 {
		return prefs.Preferences{}, prefs.ErrInvalid
	}
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(who.Tenant + "\x00" + who.Subject))
	lock := &p.emailPreferenceLocks[hash.Sum64()%uint64(len(p.emailPreferenceLocks))]
	lock.Lock()
	defer lock.Unlock()
	old := p.Prefs.Get(who.Tenant, who.Subject).Personal
	if len(personal) == 0 {
		return p.Prefs.Set(who.Tenant, who.Subject, value)
	}
	next := personal[0]
	// A stale profile form must not undo a newer opt-out, even if another request
	// originally read the same checkbox value. Only server-observed changes advance it.
	if next.Email != old.Email && next.EmailVersion != old.EmailVersion {
		return prefs.Preferences{}, prefs.ErrVersion
	}
	next.EmailVersion = old.EmailVersion
	personal = []prefs.Personal{next}
	if next.Email == old.Email {
		return p.Prefs.Set(who.Tenant, who.Subject, value, personal...)
	}
	if next.Email && !p.EmailAvailable(who) {
		return prefs.Preferences{}, fmt.Errorf("%w: email delivery is not configured for this account", prefs.ErrInvalid)
	}
	if who.Kind != auth.User {
		return prefs.Preferences{}, ErrForbidden
	}
	if old.EmailVersion >= 9007199254740991 {
		return prefs.Preferences{}, prefs.ErrVersion
	}
	next.EmailVersion++
	personal[0] = next
	attempt, err := p.BeginAudit(who, audit.Change{Route: "PUT /api/preferences", Rule: "authenticated caller; own preferences; configured human recipient for opt-in", Action: "notifications.email.preference", Resource: "notification_email", ID: who.Subject, Scope: "personal", Subject: who.Subject, Before: map[string]string{"enabled": strconv.FormatBool(old.Email)}, After: map[string]string{"enabled": strconv.FormatBool(next.Email)}})
	if err != nil {
		return prefs.Preferences{}, err
	}
	result, err := p.Prefs.Set(who.Tenant, who.Subject, value, personal...)
	if finishErr := attempt.Finish(err); finishErr != nil {
		p.LogAuditCompletionFailure(finishErr)
	}
	return result, err
}
