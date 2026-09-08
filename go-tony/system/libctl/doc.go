// Package libctl provides support code for building controllers that mount to docd.
//
// This package is built bottom-up: as patterns emerge from implementing and
// testing controllers, useful abstractions are collected here. The goal is to
// provide a foundation for controller development without over-engineering
// ahead of actual needs.
//
// Typical controller lifecycle:
//
//  1. Connect to docd via TCP
//  2. Send mount request (hello + mount with path and schema)
//  3. Receive mount confirmation
//  4. Handle PATCH operations routed from docd
//  5. Write results to logd
//  6. Clean up on shutdown
//
// # Response errors
//
// When the server answers a request with an error, that error is WRAPPED, not
// rendered. The *api.SessionError reaches the caller intact, so the code survives
// the trip and can be asked for:
//
//	node, err := sess.Match(ctx, path)
//	if api.ErrorCode(err) == api.ErrCodeNotFound {
//	    // the path holds nothing — an answer, not a failure
//	}
//
// This matters because the two are not the same answer. A read that returns
// not_found says the path is empty; a read that fails says the store could not be
// asked. A caller that cannot tell them apart has to report one as the other, and
// reporting absence as failure is the worse direction: every absent key becomes an
// outage.
//
// These calls used to format the message and DROP the code, which left message
// matching as the only discriminator available — and a message is prose that gets
// reworded. It duly was: the go-tony release that gave logd's PathError its three
// kinds rewrote "path not found" into "no value at %q: ...", and every downstream
// substring match against the old spelling silently stopped matching, turning
// absent reads back into hard failures. The code was stable across that change.
// Nothing downstream could reach it.
//
// # What a watch delivers
//
// A watch on a path is a stream about THAT PATH, and every event is rooted there
// (session protocol 2, api.ProtocolVersion). The first event carries State, the value
// at the path; each later one carries Patch, a delta of that value, which a client
// applies to what it holds with api.NextState -- the same fold the server uses, so
// comments count the same on both sides -- and lands where a fresh read of the path
// lands. Absent on an event says the path holds nothing after it: a watch that asked
// to wait starts from nothing, and a delta that removed the path is still delivered,
// with Absent said. A null in State or Patch is a null the path holds, never absence.
// A client that held document-rooted patches for a path it did not watch at the root
// has nothing to extract any more; what arrives is what to apply.
package libctl
