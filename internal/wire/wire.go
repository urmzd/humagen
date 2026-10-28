// Package wire is Humagen's one seam onto fiber's run model.
//
// It reads and writes what a run is raised with (an event's request, its
// context and the surface fields a connector typed), the lifecycle bodies a
// run publishes, and the agent definitions Humagen keeps as saige
// definitions. Every other package goes through it, so the run model can
// change in one place.
package wire
