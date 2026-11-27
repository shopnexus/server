package domain

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// An operation's x-capability says which agents may call it: none, no
// agent; turn, any agent's turn; anything else, the agents that hold that
// capability, such as tickets.read.
const (
	CapabilityNone = "none"
	CapabilityTurn = "turn"
)

// RoleAdmin is the console role an operation marked x-role: admin needs;
// any member may call the others. It is the role as module console names
// it.
const RoleAdmin = "admin"

// OperationsName is the operation the shopnexus CLI lists its commands
// with; it lists every other.
const OperationsName = "listAgentOperations"

// Operation is one console API operation an agent may be given, as the
// spec describes it and the shopnexus CLI shows it.
type Operation struct {
	// Name is the operationId without Console, the CLI's command.
	Name        string `json:"name"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	Summary     string `json:"summary"`
	Description string `json:"description,omitempty"`
	Capability  string `json:"capability"`
	// Role is RoleAdmin for an operation only an admin may call, "" for
	// one any member may.
	Role       string          `json:"role,omitempty"`
	Approval   bool            `json:"approval"`
	Parameters []Parameter     `json:"parameters"`
	Body       json.RawMessage `json:"body,omitempty"`
}

// Parameter is an operation's path or query parameter.
type Parameter struct {
	Name        string          `json:"name"`
	In          string          `json:"in"`
	Required    bool            `json:"required"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"`
}

// Catalog is every console operation an agent may be given, read from the
// spec's x-capability.
type Catalog struct {
	byRoute      map[string]Operation // "GET /v1/console/accounts/{id}"
	granted      []Operation          // by name
	capabilities []string
}

// NewCatalog reads the operations under /v1/console/ whose x-capability is
// turn or a capability. A change is an operation of any method but GET: an
// agent's call of one waits for a member's approval.
func NewCatalog(doc *openapi3.T) (Catalog, error) {
	c := Catalog{byRoute: map[string]Operation{}}
	names := map[string]string{}
	caps := map[string]bool{}
	for path, item := range doc.Paths.Map() {
		if !strings.HasPrefix(path, "/v1/console/") {
			continue
		}
		for method, op := range item.Operations() {
			capability, ok := op.Extensions["x-capability"].(string)
			if !ok || capability == "" || capability == CapabilityNone {
				continue
			}
			raw, marked := op.Extensions["x-role"]
			role, isString := raw.(string)
			if marked && (!isString || role != RoleAdmin) {
				return Catalog{}, fmt.Errorf("agent catalog: %s: x-role %v is not %s", op.OperationID, raw, RoleAdmin)
			}
			method = strings.ToUpper(method)
			o := Operation{
				Role: role,
				Name: strings.Replace(op.OperationID, "Console", "", 1), Method: method, Path: path,
				Summary: op.Summary, Description: strings.TrimSpace(op.Description), Capability: capability,
				Approval: method != http.MethodGet, Parameters: []Parameter{},
			}
			if prev, taken := names[o.Name]; taken {
				return Catalog{}, fmt.Errorf("agent catalog: %s and %s are both %s", prev, op.OperationID, o.Name)
			}
			names[o.Name] = op.OperationID
			for _, p := range slices.Concat(item.Parameters, op.Parameters) {
				if p.Value == nil || (p.Value.In != openapi3.ParameterInPath && p.Value.In != openapi3.ParameterInQuery) {
					continue
				}
				schema, err := inline(p.Value.Schema)
				if err != nil {
					return Catalog{}, fmt.Errorf("agent catalog: %s %s: %w", op.OperationID, p.Value.Name, err)
				}
				o.Parameters = append(o.Parameters, Parameter{
					Name: p.Value.Name, In: p.Value.In, Required: p.Value.Required,
					Description: strings.TrimSpace(p.Value.Description), Schema: schema,
				})
			}
			if rb := op.RequestBody; rb != nil && rb.Value != nil {
				if mt := rb.Value.Content.Get("application/json"); mt != nil {
					body, err := inline(mt.Schema)
					if err != nil {
						return Catalog{}, fmt.Errorf("agent catalog: %s body: %w", op.OperationID, err)
					}
					o.Body = body
				}
			}
			c.byRoute[method+" "+path] = o
			if capability != CapabilityTurn {
				caps[capability] = true
			}
		}
	}
	c.granted = slices.SortedFunc(maps.Values(c.byRoute), func(a, b Operation) int { return strings.Compare(a.Name, b.Name) })
	for k := range caps {
		c.capabilities = append(c.capabilities, k)
	}
	slices.Sort(c.capabilities)
	return c, nil
}

// Operation is the operation a route pattern serves ("GET",
// "/v1/console/accounts/{id}"), when an agent may be given it.
func (c Catalog) Operation(method, pattern string) (Operation, bool) {
	o, ok := c.byRoute[method+" "+pattern]
	return o, ok
}

// Capabilities are the capabilities the spec names, sorted.
func (c Catalog) Capabilities() []string { return slices.Clone(c.capabilities) }

// Known says whether the spec names capability.
func (c Catalog) Known(capability string) bool {
	_, ok := slices.BinarySearch(c.capabilities, capability)
	return ok
}

// Operations lists every operation an agent may be given, by name, without
// the CLI's own listing.
func (c Catalog) Operations() []Operation {
	out := []Operation{}
	for i := range c.granted {
		if o := &c.granted[i]; o.Name != OperationsName {
			out = append(out, *o)
		}
	}
	return out
}

// Granted lists the operations a turn of an agent holding caps, for a
// member of role, may call: Operations that Grants.
func (c Catalog) Granted(caps []string, role string) []Operation {
	all := c.Operations()
	out := make([]Operation, 0, len(all))
	for i := range all {
		if Grants(caps, role, all[i]) {
			out = append(out, all[i])
		}
	}
	return out
}

// Grants says whether a turn of an agent holding caps, for a member of
// role, may call o. An admin's operation is left out of a moderator's turn,
// so its prompt does not offer what the console would refuse.
func Grants(caps []string, role string, o Operation) bool {
	if o.Role != "" && o.Role != role {
		return false
	}
	return o.Capability == CapabilityTurn || slices.Contains(caps, o.Capability)
}

// maxDepth bounds how deep inline follows a schema, against a schema that
// refers to itself.
const maxDepth = 12

// inline is s as JSON Schema with every $ref replaced by what it names.
func inline(s *openapi3.SchemaRef) (json.RawMessage, error) {
	if s == nil {
		return nil, nil
	}
	v, err := copySchema(s, 0)
	if err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

func copySchema(s *openapi3.SchemaRef, depth int) (*openapi3.SchemaRef, error) {
	if s == nil || s.Value == nil {
		return s, nil
	}
	if depth > maxDepth {
		return nil, fmt.Errorf("schema deeper than %d", maxDepth)
	}
	v := *s.Value
	var err error
	each := func(list openapi3.SchemaRefs) (openapi3.SchemaRefs, error) {
		if list == nil {
			return nil, nil
		}
		out := make(openapi3.SchemaRefs, len(list))
		for i, r := range list {
			if out[i], err = copySchema(r, depth+1); err != nil {
				return nil, err
			}
		}
		return out, nil
	}
	if v.OneOf, err = each(v.OneOf); err != nil {
		return nil, err
	}
	if v.AnyOf, err = each(v.AnyOf); err != nil {
		return nil, err
	}
	if v.AllOf, err = each(v.AllOf); err != nil {
		return nil, err
	}
	if v.Not, err = copySchema(v.Not, depth+1); err != nil {
		return nil, err
	}
	if v.Items, err = copySchema(v.Items, depth+1); err != nil {
		return nil, err
	}
	if v.AdditionalProperties.Schema, err = copySchema(v.AdditionalProperties.Schema, depth+1); err != nil {
		return nil, err
	}
	if v.Properties != nil {
		props := make(openapi3.Schemas, len(v.Properties))
		for k, r := range v.Properties {
			if props[k], err = copySchema(r, depth+1); err != nil {
				return nil, err
			}
		}
		v.Properties = props
	}
	return &openapi3.SchemaRef{Value: &v}, nil
}
