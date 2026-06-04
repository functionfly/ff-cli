package verifier

import (
	"fmt"

	"github.com/functionfly/ff-cli/internal/flypy/ir"
)

// SideEffect represents a detected side effect in the code
// (Type, Function, Location, Operation fields inherited from base SideEffect)

// VariableScope represents the scope of a variable
type VariableScope int

const (
	ScopeLocal VariableScope = iota
	ScopeParameter
	ScopeGlobal
	ScopeClosure
)

// VariableInfo tracks information about a variable
type VariableInfo struct {
	Name  string
	Scope VariableScope
	Type  ir.IRType
}

// SideEffectAnalyzer analyzes side effects in IR modules
type SideEffectAnalyzer struct {
	Module       *ir.Module
	Effects      []SideEffect
	Globals      map[string]bool
	Closures     map[string]bool
	Variables    map[string]*VariableInfo
	FunctionVars map[string]map[string]*VariableInfo // function name -> variables
}

// NewSideEffectAnalyzer creates a new side effect analyzer
func NewSideEffectAnalyzer(module *ir.Module) *SideEffectAnalyzer {
	analyzer := &SideEffectAnalyzer{
		Module:       module,
		Effects:      make([]SideEffect, 0),
		Globals:      make(map[string]bool),
		Closures:     make(map[string]bool),
		Variables:    make(map[string]*VariableInfo),
		FunctionVars: make(map[string]map[string]*VariableInfo),
	}

	// Initialize with common global variables
	analyzer.initCommonGlobals()

	return analyzer
}

// initCommonGlobals initializes the analyzer with commonly known global variables
func (a *SideEffectAnalyzer) initCommonGlobals() {
	commonGlobals := []string{
		"__name__", "__file__", "__doc__", "__package__",
		// Add more common Python globals as needed
	}

	for _, global := range commonGlobals {
		a.Globals[global] = true
	}
}

// AddGlobal marks a variable as global
func (a *SideEffectAnalyzer) AddGlobal(name string) {
	a.Globals[name] = true
}

// AddClosure marks a variable as from a closure
func (a *SideEffectAnalyzer) AddClosure(name string) {
	a.Closures[name] = true
}

// Analyze performs side effect analysis on the module
func (a *SideEffectAnalyzer) Analyze() []SideEffect {
	for _, fn := range a.Module.Functions {
		a.analyzeFunction(fn)
	}
	return a.Effects
}

// analyzeFunction analyzes a single function for side effects
func (a *SideEffectAnalyzer) analyzeFunction(fn *ir.Function) {
	// Initialize function variable tracking
	a.FunctionVars[fn.Name] = make(map[string]*VariableInfo)

	// Track parameters
	for _, param := range fn.Parameters {
		a.FunctionVars[fn.Name][param.Name] = &VariableInfo{
			Name:  param.Name,
			Scope: ScopeParameter,
			Type:  param.Type,
		}
	}

	// Analyze operations
	for _, op := range fn.Body {
		a.analyzeOperation(fn, op)
	}
}

// analyzeOperation analyzes a single operation for side effects
func (a *SideEffectAnalyzer) analyzeOperation(fn *ir.Function, op ir.Operation) {
	switch op.Type {
	case "assign":
		a.analyzeAssignment(fn, op)
	case "return":
		// Returns don't have side effects
		return
	default:
		// Check for call operations (identified by Kind)
		if op.Kind == ir.Call {
			a.analyzeCall(fn, op)
		} else {
			// Check for other mutation operations
			a.analyzeMutationOperation(fn, op)
		}
	}
}

// analyzeAssignment analyzes assignment operations for side effects
func (a *SideEffectAnalyzer) analyzeAssignment(fn *ir.Function, op ir.Operation) {
	if op.Result == "" {
		return
	}

	// Track the variable assignment
	varInfo := &VariableInfo{
		Name:  op.Result,
		Scope: ScopeLocal, // Default to local
		Type:  op.Type_,
	}

	// Check if this is a parameter being reassigned
	if a.isParameterMutation(fn, op.Result) {
		varInfo.Scope = ScopeParameter
		a.addEffect(SideEffectMutation, fn.Name, "assignment", op.Result,
			fmt.Sprintf("mutation of parameter '%s'", op.Result))
	} else if a.isGlobalVariable(op.Result) {
		varInfo.Scope = ScopeGlobal
		a.addEffect(SideEffectMutation, fn.Name, "assignment", op.Result,
			fmt.Sprintf("assignment to global variable '%s'", op.Result))
	} else if a.isClosureVariable(fn, op.Result) {
		varInfo.Scope = ScopeClosure
		a.addEffect(SideEffectMutation, fn.Name, "assignment", op.Result,
			fmt.Sprintf("assignment to closure variable '%s'", op.Result))
	}

	// Store variable info
	a.FunctionVars[fn.Name][op.Result] = varInfo
}

// analyzeCall analyzes function calls for side effects
func (a *SideEffectAnalyzer) analyzeCall(fn *ir.Function, op ir.Operation) {
	// Check if this is a function call operation
	if op.Kind == ir.Call {
		if callValue, ok := op.Value.(map[string]interface{}); ok {
			if calledFn, ok := callValue["func"].(string); ok {
				// Check for network operations
				if a.isNetworkOperation(calledFn) {
					a.addEffect(SideEffectNetwork, fn.Name, "call", calledFn,
						fmt.Sprintf("network operation: %s()", calledFn))
				}

				// Check for external state operations
				if a.isExternalStateOperation(calledFn) {
					a.addEffect(SideEffectExternalState, fn.Name, "call", calledFn,
						fmt.Sprintf("external state access: %s()", calledFn))
				}

				// Check for I/O operations
				if a.isIOOperation(calledFn) {
					a.addEffect(SideEffectIO, fn.Name, "call", calledFn,
						fmt.Sprintf("I/O operation: %s()", calledFn))
				}
			}
		}
	}
}

// analyzeMutationOperation analyzes other operations that might cause mutations
func (a *SideEffectAnalyzer) analyzeMutationOperation(fn *ir.Function, op ir.Operation) {
	// Check for operations that modify collections in place
	switch op.Type {
	case "list_append", "list_extend", "list_insert", "list_remove", "list_pop", "list_clear":
		a.analyzeCollectionMutation(fn, op, "list")
	case "dict_set", "dict_del", "dict_clear":
		a.analyzeCollectionMutation(fn, op, "dict")
	case "set_add", "set_remove", "set_clear":
		a.analyzeCollectionMutation(fn, op, "set")
	}
}

// analyzeCollectionMutation analyzes mutations to collection types
func (a *SideEffectAnalyzer) analyzeCollectionMutation(fn *ir.Function, op ir.Operation, collectionType string) {
	if len(op.Operands) == 0 {
		return
	}

	target := a.getTargetName(op.Operands[0])
	if target == "" {
		return
	}

	// Check if this is mutating a parameter
	if a.isParameterMutation(fn, target) {
		a.addEffect(SideEffectMutation, fn.Name, op.Type, target,
			fmt.Sprintf("mutation of parameter %s '%s' via %s", collectionType, target, op.Type))
	}

	// Check if this is mutating a global or closure variable
	if a.isGlobalVariable(target) {
		a.addEffect(SideEffectMutation, fn.Name, op.Type, target,
			fmt.Sprintf("mutation of global %s '%s' via %s", collectionType, target, op.Type))
	}

	if a.isClosureVariable(fn, target) {
		a.addEffect(SideEffectMutation, fn.Name, op.Type, target,
			fmt.Sprintf("mutation of closure %s '%s' via %s", collectionType, target, op.Type))
	}
}

// isGlobalVariable checks if a variable is global
func (a *SideEffectAnalyzer) isGlobalVariable(name string) bool {
	return a.Globals[name]
}

// isClosureVariable checks if a variable is from a closure
func (a *SideEffectAnalyzer) isClosureVariable(fn *ir.Function, name string) bool {
	return a.Closures[name]
}

// isParameterMutation checks if assignment is mutating a parameter
func (a *SideEffectAnalyzer) isParameterMutation(fn *ir.Function, name string) bool {
	for _, param := range fn.Parameters {
		if param.Name == name {
			return true
		}
	}
	return false
}

// isNetworkOperation checks if a function call is a network operation
func (a *SideEffectAnalyzer) isNetworkOperation(funcName string) bool {
	networkOps := map[string]bool{
		// HTTP requests
		"requests.get":                true,
		"requests.post":               true,
		"requests.put":                true,
		"requests.delete":             true,
		"requests.patch":              true,
		"requests.head":               true,
		"requests.options":            true,
		"requests.request":            true,
		"urllib.request.urlopen":      true,
		"urllib.request.Request":      true,
		"http.client.HTTPConnection":  true,
		"http.client.HTTPSConnection": true,

		// Sockets and low-level networking
		"socket.socket":  true,
		"socket.connect": true,
		"socket.bind":    true,
		"socket.listen":  true,
		"socket.accept":  true,
		"socket.send":    true,
		"socket.recv":    true,

		// WebSocket
		"websockets.connect":          true,
		"websocket.create_connection": true,

		// Database connections (network)
		"psycopg2.connect":        true,
		"sqlite3.connect":         true, // Can be network if using network filesystems
		"mysql.connector.connect": true,
		"pymongo.MongoClient":     true,

		// RPC and remote calls
		"xmlrpc.client.ServerProxy": true,
		"grpc.insecure_channel":     true,
		"grpc.secure_channel":       true,
	}
	return networkOps[funcName]
}

// isExternalStateOperation checks if a function accesses external state
func (a *SideEffectAnalyzer) isExternalStateOperation(funcName string) bool {
	externalOps := map[string]bool{
		// File operations
		"open":       true,
		"file.read":  true,
		"file.write": true,
		"file.close": true,
		"io.open":    true,
		"io.read":    true,
		"io.write":   true,

		// OS operations
		"os.open":    true,
		"os.read":    true,
		"os.write":   true,
		"os.close":   true,
		"os.remove":  true,
		"os.rename":  true,
		"os.mkdir":   true,
		"os.rmdir":   true,
		"os.listdir": true,
		"os.chdir":   true,
		"os.getcwd":  true,
		"os.environ": true, // Environment variables

		// System operations
		"sys.exit":         true,
		"sys.stdout.write": true,
		"sys.stderr.write": true,

		// Time operations
		"time.time":                true,
		"time.sleep":               true,
		"time.ctime":               true,
		"time.gmtime":              true,
		"time.localtime":           true,
		"datetime.datetime.now":    true,
		"datetime.datetime.utcnow": true,

		// Random operations
		"random.random":  true,
		"random.randint": true,
		"random.choice":  true,
		"random.shuffle": true,
		"random.seed":    true,

		// UUID generation
		"uuid.uuid1": true,
		"uuid.uuid4": true,
		"uuid.uuid3": true,
		"uuid.uuid5": true,

		// Temporary files
		"tempfile.NamedTemporaryFile": true,
		"tempfile.TemporaryFile":      true,
		"tempfile.mkdtemp":            true,
		"tempfile.mkstemp":            true,

		// File utilities
		"shutil.copy":             true,
		"shutil.move":             true,
		"shutil.rmtree":           true,
		"glob.glob":               true,
		"pathlib.Path":            true,
		"pathlib.Path.exists":     true,
		"pathlib.Path.read_text":  true,
		"pathlib.Path.write_text": true,

		// Configuration and settings
		"configparser.ConfigParser.read":  true,
		"configparser.ConfigParser.write": true,
	}
	return externalOps[funcName]
}

// isIOOperation checks if a function performs I/O
func (a *SideEffectAnalyzer) isIOOperation(funcName string) bool {
	ioOps := map[string]bool{
		// Console I/O
		"print":     true,
		"input":     true,
		"raw_input": true,

		// Logging
		"logging.debug":      true,
		"logging.info":       true,
		"logging.warning":    true,
		"logging.error":      true,
		"logging.critical":   true,
		"logging.log":        true,
		"logging.Logger.log": true,

		// Serialization
		"json.dump":      true,
		"json.dumps":     true,
		"json.load":      true,
		"json.loads":     true,
		"pickle.dump":    true,
		"pickle.dumps":   true,
		"pickle.load":    true,
		"pickle.loads":   true,
		"yaml.dump":      true,
		"yaml.safe_dump": true,
		"yaml.load":      true,
		"yaml.safe_load": true,

		// CSV operations
		"csv.reader":     true,
		"csv.writer":     true,
		"csv.DictReader": true,
		"csv.DictWriter": true,
	}
	return ioOps[funcName]
}

// getTargetName extracts the target name from an operand
func (a *SideEffectAnalyzer) getTargetName(operand ir.Value) string {
	if operand.Kind == ir.Reference {
		if name, ok := operand.Value.(string); ok {
			return name
		}
	}
	return ""
}

// addEffect adds a side effect to the results
func (a *SideEffectAnalyzer) addEffect(effectType SideEffectType, function, operation, target, message string) {
	a.Effects = append(a.Effects, SideEffect{
		Type:      effectType,
		Function:  function,
		Location:  function, // For now, location is the function name
		Operation: operation,
		Target:    target,
		Message:   message,
	})
}

// IsIdempotent checks if a function is idempotent (produces same result for same input)
func (a *SideEffectAnalyzer) IsIdempotent(fn *ir.Function) bool {
	// A function is idempotent if it has no side effects that would change external state
	// or if all side effects are deterministic and don't affect future calls
	for _, effect := range a.Effects {
		if effect.Function == fn.Name {
			switch effect.Type {
			case SideEffectNetwork, SideEffectExternalState, SideEffectIO:
				return false
			case SideEffectMutation:
				// Parameter mutations are allowed for idempotency (they don't affect external state)
				// But global/closure mutations make it non-idempotent
				if effect.Target != "" {
					if varInfo, exists := a.FunctionVars[fn.Name][effect.Target]; exists {
						if varInfo.Scope == ScopeGlobal || varInfo.Scope == ScopeClosure {
							return false
						}
					}
				}
			}
		}
	}
	return true
}

// IsPure checks if a function is pure (no side effects at all)
func (a *SideEffectAnalyzer) IsPure(fn *ir.Function) bool {
	for _, effect := range a.Effects {
		if effect.Function == fn.Name {
			return false
		}
	}
	return true
}

// GetFunctionSideEffects returns all side effects for a specific function
func (a *SideEffectAnalyzer) GetFunctionSideEffects(functionName string) []SideEffect {
	var functionEffects []SideEffect
	for _, effect := range a.Effects {
		if effect.Function == functionName {
			functionEffects = append(functionEffects, effect)
		}
	}
	return functionEffects
}

// HasExternalSideEffects checks if a function has external side effects
func (a *SideEffectAnalyzer) HasExternalSideEffects(fn *ir.Function) bool {
	for _, effect := range a.Effects {
		if effect.Function == fn.Name {
			if effect.Type == SideEffectNetwork || effect.Type == SideEffectExternalState || effect.Type == SideEffectIO {
				return true
			}
		}
	}
	return false
}

// GetDeterministicFunctions returns all functions that are deterministic
func (a *SideEffectAnalyzer) GetDeterministicFunctions() []*ir.Function {
	var deterministic []*ir.Function
	for _, fn := range a.Module.Functions {
		if a.IsIdempotent(fn) {
			deterministic = append(deterministic, fn)
		}
	}
	return deterministic
}

// GetPureFunctions returns all functions that are pure (no side effects)
func (a *SideEffectAnalyzer) GetPureFunctions() []*ir.Function {
	var pure []*ir.Function
	for _, fn := range a.Module.Functions {
		if a.IsPure(fn) {
			pure = append(pure, fn)
		}
	}
	return pure
}

// GetSideEffectsByType returns side effects filtered by type
func (a *SideEffectAnalyzer) GetSideEffectsByType(effectType SideEffectType) []SideEffect {
	var filtered []SideEffect
	for _, effect := range a.Effects {
		if effect.Type == effectType {
			filtered = append(filtered, effect)
		}
	}
	return filtered
}

// HasSideEffects checks if the module has any side effects
func (a *SideEffectAnalyzer) HasSideEffects() bool {
	return len(a.Effects) > 0
}

// GetSideEffectSummary returns a summary of side effects by type
func (a *SideEffectAnalyzer) GetSideEffectSummary() map[SideEffectType]int {
	summary := make(map[SideEffectType]int)
	for _, effect := range a.Effects {
		summary[effect.Type]++
	}
	return summary
}
