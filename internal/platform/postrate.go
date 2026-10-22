package platform

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrPostRate reports a person, or a project, posting messages faster than a
// thread takes them. A person's message starts no run, so no limit on runs
// holds it: this one does.
var ErrPostRate = errors.New("platform: too many messages")

// How fast people post in threads: each person at most PostsPerPerson
// messages a PostWindow in a workspace, and each project, its root
// included, at most PostsPerProject. At most PostBuckets windows are kept
// at once; past that, with none ended, a post is refused rather than a
// window forgotten.
const (
	PostWindow      = time.Minute
	PostsPerPerson  = 30
	PostsPerProject = 120
	PostBuckets     = 10_000
)

// postRate counts the messages posted in each window, by person and by
// project. Its zero value is ready.
type postRate struct {
	mu      sync.Mutex
	windows map[postKey]postWindow
}

type postKey struct {
	tenant, kind, name string
}

type postWindow struct {
	start time.Time
	n     int
}

// take counts one message by a person in a project, or refuses it, saying
// which limit it would pass. Nothing is counted for a message refused.
func (r *postRate) take(tenant, person, project string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.windows == nil {
		r.windows = map[postKey]postWindow{}
	}
	keys := [2]postKey{{tenant, "person", person}, {tenant, "project", project}}
	limits := [2]int{PostsPerPerson, PostsPerProject}
	var next [2]postWindow
	for i, k := range keys {
		w, ok := r.windows[k]
		if !ok || now.Sub(w.start) >= PostWindow {
			w = postWindow{start: now}
		}
		if w.n >= limits[i] {
			wait := max(time.Second, w.start.Add(PostWindow).Sub(now).Round(time.Second))
			if i == 0 {
				return fmt.Errorf("%w: you have posted %d messages in the last minute; try again in %s", ErrPostRate, limits[i], wait)
			}
			return fmt.Errorf("%w: this project has taken %d messages in the last minute; try again in %s", ErrPostRate, limits[i], wait)
		}
		w.n++
		next[i] = w
	}
	missing := func() int {
		n := 0
		for _, k := range keys {
			if _, ok := r.windows[k]; !ok {
				n++
			}
		}
		return n
	}
	if len(r.windows)+missing() > PostBuckets {
		for k, w := range r.windows {
			if now.Sub(w.start) >= PostWindow {
				delete(r.windows, k)
			}
		}
		if len(r.windows)+missing() > PostBuckets {
			return fmt.Errorf("%w: too many people are posting at once; try again in a minute", ErrPostRate)
		}
	}
	for i, k := range keys {
		r.windows[k] = next[i]
	}
	return nil
}
