package snap

import (
	"math/rand"
	"strconv"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/stream"
)

// RandomDocConfig configures random document generation
type RandomDocConfig struct {
	// MinSize and MaxSize control the approximate size range in bytes
	MinSize int
	MaxSize int
	
	// MaxDepth controls maximum nesting depth
	MaxDepth int
	
	// ObjectFieldProbability is probability (0.0-1.0) that a container will be an object vs array
	ObjectFieldProbability float64
	
	// ContainerProbability is probability (0.0-1.0) that a value will be a container vs primitive
	ContainerProbability float64
	
	// StringLengthRange controls string value lengths
	StringLengthMin int
	StringLengthMax int
	
	// Seed for random number generator (0 means use current time)
	Seed int64
}

// DefaultRandomDocConfig returns a reasonable default configuration
func DefaultRandomDocConfig() RandomDocConfig {
	return RandomDocConfig{
		MinSize:               100000,  // 100KB
		MaxSize:               10000000, // 10MB
		MaxDepth:              5,
		ObjectFieldProbability: 0.6,
		ContainerProbability:   0.3,
		StringLengthMin:       10,
		StringLengthMax:       1000,
		Seed:                  0,
	}
}

// RandomDocument generates a random document with mixed structure
// Returns the document as an ir.Node and all paths that exist in it
func RandomDocument(config RandomDocConfig) (*ir.Node, []string, error) {
	if config.Seed == 0 {
		config.Seed = rand.Int63()
	}
	rng := rand.New(rand.NewSource(config.Seed))
	
	targetSize := config.MinSize
	if config.MaxSize > config.MinSize {
		targetSize = config.MinSize + rng.Intn(config.MaxSize-config.MinSize)
	}
	
	// Generate document
	doc, paths, size := generateRandomNode(rng, config, 0, targetSize)
	
	// If document is too small, add more fields
	for size < targetSize {
		// Add more top-level fields
		if doc.Type == ir.ObjectType {
			// Convert to map, add field, convert back
			objMap := ir.ToMap(doc)
			if objMap == nil {
				objMap = make(map[string]*ir.Node)
			}
			key := "field_" + strconv.Itoa(len(objMap))
			value, subPaths, valueSize := generateRandomNode(rng, config, 0, targetSize-size)
			objMap[key] = value
			doc = ir.FromMap(objMap)
			paths = append(paths, key)
			for _, p := range subPaths {
				paths = append(paths, key+"."+p)
			}
			size += valueSize
		} else if doc.Type == ir.ArrayType {
			// Document is an array, add more elements
			elements := doc.Values
			value, subPaths, valueSize := generateRandomNode(rng, config, 0, targetSize-size)
			elements = append(elements, value)
			doc = ir.FromSlice(elements)
			idx := len(elements) - 1
			paths = append(paths, "["+strconv.Itoa(idx)+"]")
			for _, p := range subPaths {
				paths = append(paths, "["+strconv.Itoa(idx)+"]."+p)
			}
			size += valueSize
		} else {
			// Document is a primitive, wrap it in an object
			key := "value"
			doc = ir.FromMap(map[string]*ir.Node{key: doc})
			paths = []string{key}
		}
		if size >= targetSize {
			break
		}
	}
	
	return doc, paths, nil
}

// generateRandomNode generates a random node recursively
// Returns the node, all paths within it (relative to this node), and approximate size
func generateRandomNode(rng *rand.Rand, config RandomDocConfig, depth int, remainingSize int) (*ir.Node, []string, int) {
	var paths []string
	size := 0
	
	// Decide if this should be a container or primitive
	isContainer := depth < config.MaxDepth && rng.Float64() < config.ContainerProbability && remainingSize > 1000
	
	if isContainer {
		isObject := rng.Float64() < config.ObjectFieldProbability
		
		if isObject {
			// Generate object using FromMap helper
			objMap := make(map[string]*ir.Node)
			size += 2 // {} brackets
			
			// Generate fields
			numFields := 5 + rng.Intn(20) // 5-25 fields
			for i := 0; i < numFields && remainingSize > 100; i++ {
				key := randomKey(rng, i)
				fieldSize := remainingSize / (numFields - i) // Distribute remaining size
				value, subPaths, valueSize := generateRandomNode(rng, config, depth+1, fieldSize)
				objMap[key] = value
				paths = append(paths, key)
				for _, p := range subPaths {
					paths = append(paths, key+"."+p)
				}
				size += len(key) + valueSize + 5 // key + value + overhead
				remainingSize -= valueSize
			}
			
			node := ir.FromMap(objMap)
			return node, paths, size
		} else {
			// Generate array using FromSlice helper
			elements := []*ir.Node{}
			size += 2 // [] brackets
			
			// Generate elements
			numElements := 3 + rng.Intn(15) // 3-18 elements
			for i := 0; i < numElements && remainingSize > 100; i++ {
				elementSize := remainingSize / (numElements - i)
				value, subPaths, valueSize := generateRandomNode(rng, config, depth+1, elementSize)
				elements = append(elements, value)
				idx := len(elements) - 1
				paths = append(paths, "["+strconv.Itoa(idx)+"]")
				for _, p := range subPaths {
					paths = append(paths, "["+strconv.Itoa(idx)+"]."+p)
				}
				size += valueSize + 2 // value + overhead
				remainingSize -= valueSize
			}
			
			node := ir.FromSlice(elements)
			return node, paths, size
		}
	} else {
		// Generate primitive value
		valueType := rng.Intn(5)
		switch valueType {
		case 0: // String
			length := config.StringLengthMin + rng.Intn(config.StringLengthMax-config.StringLengthMin)
			value := randomString(rng, length)
			size += length + 2 // quotes
			return &ir.Node{Type: ir.StringType, String: value}, []string{}, size
		case 1: // Int
			value := int64(rng.Intn(1000000))
			size += 10 // approximate
			return &ir.Node{Type: ir.NumberType, Int64: &value}, []string{}, size
		case 2: // Float
			value := rng.Float64() * 1000000
			size += 15 // approximate
			return &ir.Node{Type: ir.NumberType, Float64: &value}, []string{}, size
		case 3: // Bool
			value := rng.Float64() < 0.5
			size += 5
			return &ir.Node{Type: ir.BoolType, Bool: value}, []string{}, size
		case 4: // Null
			size += 4
			return &ir.Node{Type: ir.NullType}, []string{}, size
		}
	}
	
	// Fallback
	return &ir.Node{Type: ir.NullType}, []string{}, 4
}

// randomKey generates a random object key
func randomKey(rng *rand.Rand, index int) string {
	// Mix of patterns to create interesting paths
	switch rng.Intn(4) {
	case 0:
		return "field_" + strconv.Itoa(index)
	case 1:
		return "key" + strconv.Itoa(index)
	case 2:
		return "data_" + strconv.Itoa(index) + "_value"
	case 3:
		return "nested_" + strconv.Itoa(index) + "_field"
	default:
		return "field_" + strconv.Itoa(index)
	}
}

// randomString generates a random string of given length
func randomString(rng *rand.Rand, length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 _-"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rng.Intn(len(charset))]
	}
	return string(b)
}

// EncodeRandomDocument encodes a random document to events using the stream encoder
func EncodeRandomDocument(doc *ir.Node, enc *stream.Encoder) error {
	return encodeNodeToEvents(doc, enc)
}

// encodeNodeToEvents recursively encodes a node to events
func encodeNodeToEvents(node *ir.Node, enc *stream.Encoder) error {
	if node == nil {
		return enc.WriteNull()
	}
	
	switch node.Type {
	case ir.ObjectType:
		if err := enc.BeginObject(); err != nil {
			return err
		}
		// Fields and Values are parallel arrays - Fields[i] is the key, Values[i] is the value
		for i := range node.Fields {
			keyNode := node.Fields[i]
			if keyNode == nil || keyNode.Type != ir.StringType {
				continue
			}
			key := keyNode.String
			if err := enc.WriteKey(key); err != nil {
				return err
			}
			if i < len(node.Values) {
				if err := encodeNodeToEvents(node.Values[i], enc); err != nil {
					return err
				}
			} else {
				if err := enc.WriteNull(); err != nil {
					return err
				}
			}
		}
		return enc.EndObject()
		
	case ir.ArrayType:
		if err := enc.BeginArray(); err != nil {
			return err
		}
		for _, elem := range node.Values {
			if err := encodeNodeToEvents(elem, enc); err != nil {
				return err
			}
		}
		return enc.EndArray()
		
	case ir.StringType:
		return enc.WriteString(node.String)
		
	case ir.NumberType:
		if node.Int64 != nil {
			return enc.WriteInt(*node.Int64)
		}
		if node.Float64 != nil {
			return enc.WriteFloat(*node.Float64)
		}
		return enc.WriteNull()
		
	case ir.BoolType:
		return enc.WriteBool(node.Bool)
		
	case ir.NullType:
		return enc.WriteNull()
		
	default:
		return enc.WriteNull()
	}
}
