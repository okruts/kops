package dns

// Context represents a state of the world for DNS.
// It is grouped by scopes & named keys, and controllers will replace those groups
// The DNS controller will then merge all those record sets, resolve aliases etc,
// and then configure a dns backend to match the desired state of the world.
type Context interface {
	// Replace sets the records for scope & record to the provided set of records.
	Replace(scopeName string, recordName string, records []Record)

	// MarkReady should be called when a controller has populated all the records for a particular scope (with ready=true)
	// It should also be called on creation of the controller with ready=false,
	// so that the dns controller will wait for it to become ready
	MarkReady(scopeName string, ready bool)
}
