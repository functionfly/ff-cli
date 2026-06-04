package ir

import "fmt"

// IRType represents the type of an IR value
//
//nolint:revive // IRType is used to avoid stuttering with package name 'ir' in some contexts, but kept for clarity
type IRType struct {
	Base    string  // "int", "float", "string", "bool", "list", "dict", "none", "unknown", "bytes", "object"
	Element *IRType // For lists: element type, for dicts: value type
	Key     *IRType // For dicts: key type (usually string)
	IsInput bool    // True if this comes from input parameters (should be serde_json::Value)
}

func (t IRType) String() string {
	if t.Element == nil {
		return t.Base
	}
	if t.Base == "list" {
		return fmt.Sprintf("list[%s]", t.Element.String())
	}
	if t.Base == "dict" {
		keyStr := "string"
		if t.Key != nil {
			keyStr = t.Key.String()
		}
		return fmt.Sprintf("dict[%s,%s]", keyStr, t.Element.String())
	}
	return t.Base
}

// Equals checks if two IRTypes are equal
func (t IRType) Equals(other IRType) bool {
	if t.Base != other.Base || t.IsInput != other.IsInput {
		return false
	}
	if t.Element == nil && other.Element == nil {
		return true
	}
	if t.Element == nil || other.Element == nil {
		return false
	}
	if !t.Element.Equals(*other.Element) {
		return false
	}
	if t.Key == nil && other.Key == nil {
		return true
	}
	if t.Key == nil || other.Key == nil {
		return false
	}
	return t.Key.Equals(*other.Key)
}

// Predefined types for convenience
var (
	IRTypeInt     = IRType{Base: "int"}
	IRTypeFloat   = IRType{Base: "float"}
	IRTypeString  = IRType{Base: "string"}
	IRTypeBool    = IRType{Base: "bool"}
	IRTypeList    = IRType{Base: "list", Element: &IRTypeString}                     // Default list of strings
	IRTypeDict    = IRType{Base: "dict", Key: &IRTypeString, Element: &IRTypeString} // Default dict
	IRTypeNone    = IRType{Base: "none"}
	IRTypeUnknown = IRType{Base: "unknown"}
	IRTypeBytes   = IRType{Base: "bytes"}
	IRTypeObject  = IRType{Base: "object"}
)

// Operation types for complex modules
const (
	// Standard operations
	OpAssign    = "assign"
	OpReturn    = "return"
	OpCall      = "call"
	OpBinOp     = "binop"
	OpUnaryOp   = "unaryop"
	OpCompare   = "compare"
	OpBoolOp    = "boolop"
	OpSubscript = "subscript"
	OpDict      = "dict"
	OpList      = "list"
	OpIf        = "if"
	OpFor       = "for"
	OpWhile     = "while"
	OpTry       = "try"
	OpRaise     = "raise"

	// CSV module operations (complex mode)
	OpCSVReader        = "csv_reader"
	OpCSVWriter        = "csv_writer"
	OpCSVReadRow       = "csv_read_row"
	OpCSVWriteRow      = "csv_write_row"
	OpCSVGetFieldnames = "csv_get_fieldnames"

	// IO module operations (complex mode)
	OpStringIO   = "string_io"
	OpBytesIO    = "bytes_io"
	OpIOWrite    = "io_write"
	OpIORead     = "io_read"
	OpIOGetValue = "io_getvalue"
	OpIOSeek     = "io_seek"
	OpIOTell     = "io_tell"

	// Regex module operations (complex mode)
	OpReMatch   = "re_match"
	OpReSearch  = "re_search"
	OpReSub     = "re_sub"
	OpReFindall = "re_findall"
	OpReSplit   = "re_split"
	OpReCompile = "re_compile"

	// Datetime module operations (complex mode)
	OpDatetimeParse  = "datetime_parse"
	OpDatetimeFormat = "datetime_format"
	OpDatetimeNow    = "datetime_now"
	OpTimedelta      = "timedelta"
	OpDateAdd        = "date_add"
	OpDateSub        = "date_sub"

	// JSON operations (all modes)
	OpJSONLoads = "json_loads"
	OpJSONDumps = "json_dumps"

	// Hash operations (complex mode)
	OpHashMD5    = "hash_md5"
	OpHashSHA256 = "hash_sha256"

	// Base64 operations (complex mode)
	OpBase64Encode = "base64_encode"
	OpBase64Decode = "base64_decode"
)

// Module represents an IR module (collection of functions)
type Module struct {
	Name      string
	Functions []*Function
	Imports   []string
	Metadata  map[string]interface{}
	Mode      string // "deterministic", "complex", or "compatible"
}

// Function represents an IR function
type Function struct {
	Name          string
	Parameters    []Parameter
	Body          []Operation
	ReturnType    IRType
	Pure          bool
	Deterministic bool
}

// Parameter represents a function parameter
type Parameter struct {
	Name         string
	Type         IRType
	DefaultValue interface{}
}

// Operation represents an IR operation
type Operation struct {
	Type     string
	Result   string
	Operands []Value
	Type_    IRType
	Kind     ValueKind
	Value    interface{}
	Module   string // Module name for module-specific operations (e.g., "csv", "io", "re")
	Method   string // Method name for method calls

	// Block-structured control flow support
	Body      []Operation // For if/for/while body
	ElseBody  []Operation // For if/else
	Target    string      // For loop variable (for x in ...)
	Iterator  Value       // For loop iterator
	Condition Value       // For while/if condition
	HasElse   bool        // Track if if-statement has else branch

	// Try/except support
	Handlers    []ExceptionHandler // For try/except
	FinallyBody []Operation        // For finally block
}

// ExceptionHandler represents an except handler
type ExceptionHandler struct {
	ExceptionType string      // Exception type name (e.g., "ValueError", "Exception")
	VarName       string      // Variable name for "as e" clause
	Body          []Operation // Handler body
}

// Value represents an IR value (literal or reference)
type Value struct {
	Type  IRType
	Kind  ValueKind
	Value interface{}
	// CanUnwrap indicates this value can be unwrapped by the backend
	// For example, io.StringIO(x) can be unwrapped to just x when passed to csv.DictReader
	CanUnwrap bool
	// UnwrapTo specifies what type to unwrap to: "string", "bytes", etc.
	UnwrapTo string
}

// ValueKind represents the kind of value
type ValueKind int

const (
	Literal ValueKind = iota
	Reference
	ParameterRef
	Call
	BinOp
	UnaryOp
	Compare
	BoolOp
	Subscript
	Dict
	List
	Index
	Slice
	ModuleCall // For module function calls like csv.reader()
	MethodCall // For method calls like output.getvalue()
	ListComp   // For list comprehensions
	DictComp   // For dict comprehensions
	FString    // For f-string formatted strings
)
