// Package wire is Humagen's one seam onto fiber's run model.
//
// It is the only code that reads or writes a tag's text, the surface keys a
// connector writes into a tag's meta, the event kinds a run publishes and the
// bodies they carry, and the agent definitions fiber's registry keeps. Every
// other package goes through it, so the run model can change in one place.
package wire
