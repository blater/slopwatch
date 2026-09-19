package sourceestimate

// Recognize only declared Java library contracts. A user type named Map or a
// method merely spelled computeIfAbsent/close never supplies this contract.

// The Map contract invokes a factory only on a missing key and retains the
// returned value. Count connected insertion layers, not callback body size.
// A disconnected lambda is never traversed or treated as an executed duty.

// JDBC close is a library ownership contract. Separate catch scopes establish
// that failure of one resource does not suppress the other cleanup attempt.

// A loop over an owned returned representation whose elements receive composed
// values establishes a connected possible projection duty. Getter/setter
// implementations may be absent, so retain this as an explicit estimate rather
// than claiming a proven mutation or library method contract.
