package stackeval

import (
	"context"
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/terraform/internal/lang"
	"github.com/hashicorp/terraform/internal/stacks/stackaddrs"
	"github.com/hashicorp/terraform/internal/tfdiags"
	"github.com/zclconf/go-cty/cty"
)

type EvalPhase rune

//go:generate go run golang.org/x/tools/cmd/stringer -type EvalPhase

const (
	NoPhase       EvalPhase = 0
	ValidatePhase EvalPhase = 'V'
	PlanPhase     EvalPhase = 'P'
	ApplyPhase    EvalPhase = 'A'
)

// Referenceable is implemented by types that are identified by the
// implementations of [stackaddrs.Referenceable], returning the value that
// should be used to resolve a reference to that object in an expression
// elsewhere in the configuration.
type Referenceable interface {
	// ExprReferenceValue returns the value that a reference to this object
	// should resolve to during expression evaluation.
	//
	// This method cannot fail, because it's not the expression evaluator's
	// responsibility to report errors or warnings that might arise while
	// processing the target object. Instead, this method will respond to
	// internal problems by returning a suitable placeholder value, and
	// assume that diagnostics will be returned by another concurrent
	// call path.
	ExprReferenceValue(ctx context.Context, phase EvalPhase) cty.Value
}

// ExpressionScope is implemented by types that can have expressions evaluated
// within them, providing the rules for mapping between references in
// expressions to the underlying objects that will provide their values.
type ExpressionScope interface {
	// ResolveExpressionReference decides what a particular expression reference
	// means in the receiver's evaluation scope and returns the concrete object
	// that the address is referring to.
	ResolveExpressionReference(ctx context.Context, ref stackaddrs.Reference) (Referenceable, tfdiags.Diagnostics)
}

// EvalExpr evaluates the given HCL expression in the given expression scope
// and returns the resulting value.
func EvalExpr(ctx context.Context, expr hcl.Expression, phase EvalPhase, scope ExpressionScope) (cty.Value, tfdiags.Diagnostics) {
	var diags tfdiags.Diagnostics
	traversals := expr.Variables()
	refs := make(map[stackaddrs.Referenceable]Referenceable)
	for _, traversal := range traversals {
		ref, _, moreDiags := stackaddrs.ParseReference(traversal)
		diags = diags.Append(moreDiags)
		if moreDiags.HasErrors() {
			continue
		}
		obj, moreDiags := scope.ResolveExpressionReference(ctx, ref)
		diags = diags.Append(moreDiags)
		if moreDiags.HasErrors() {
			continue
		}
		refs[ref.Target] = obj
	}
	if diags.HasErrors() {
		return cty.DynamicVal, diags
	}

	varVals := make(map[string]cty.Value)
	localVals := make(map[string]cty.Value)
	componentVals := make(map[string]cty.Value)
	stackVals := make(map[string]cty.Value)
	// TODO: Also providerVals

	for addr, obj := range refs {
		val := obj.ExprReferenceValue(ctx, phase)
		switch addr := addr.(type) {
		case stackaddrs.InputVariable:
			varVals[addr.Name] = val
		case stackaddrs.LocalValue:
			localVals[addr.Name] = val
		case stackaddrs.Component:
			componentVals[addr.Name] = val
		case stackaddrs.StackCall:
			stackVals[addr.Name] = val
		case stackaddrs.ProviderConfigRef:
			// TODO: Implement
			panic(fmt.Sprintf("don't know how to place %T in expression scope", addr))
		default:
			// The above should cover all possible referenceable address types.
			panic(fmt.Sprintf("don't know how to place %T in expression scope", addr))
		}
	}

	// HACK: The top-level lang package bundles together the problem
	// of resolving variables with the generation of the functions table.
	// We only need the functions table here, so we're going to make a
	// pseudo-scope just to load the functions from.
	// FIXME: Separate these concerns better so that both languages can
	// use the same functions but have entirely separate implementations
	// of what data is in scope.
	fakeScope := &lang.Scope{
		Data:        nil, // not a real scope; can't actually make an evalcontext
		BaseDir:     ".",
		PureOnly:    phase != ApplyPhase,
		ConsoleMode: false,
		// TODO: PlanTimestamp
	}
	hclCtx := &hcl.EvalContext{
		Variables: map[string]cty.Value{
			"var":       cty.ObjectVal(varVals),
			"local":     cty.ObjectVal(localVals),
			"component": cty.ObjectVal(componentVals),
			"stack":     cty.ObjectVal(stackVals),
			// TODO: "provider": cty.ObjectVal(providerVals),
		},
		Functions: fakeScope.Functions(),
	}

	val, hclDiags := expr.Value(hclCtx)
	diags = diags.Append(hclDiags)
	if val == cty.NilVal {
		val = cty.DynamicVal // just so the caller can assume the result is always a value
	}
	return val, diags
}
