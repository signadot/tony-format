# JSON Indexing in Existing Databases: Survey

## Overview

This document surveys how existing databases store and index JSON data, to inform our design decisions for snapshot path indexing.

## Common Approaches

### 1. MongoDB (BSON + Indexes)

**Storage**:
- Stores documents as BSON (Binary JSON)
- Documents stored as complete units
- Can index specific fields/paths

**Indexing**:
- **Field indexes**: Index specific fields (e.g., `db.collection.createIndex({ "user.name": 1 })`)
- **Compound indexes**: Multiple fields
- **Text indexes**: Full-text search
- **Geospatial indexes**: Location data

**Path Access**:
- Can query nested paths: `db.collection.find({ "user.address.city": "NYC" })`
- Indexes can be created on nested paths
- **Read**: Must read entire document, then extract path (or use projection)

**Key Insight**: MongoDB reads entire documents, but uses indexes to find which documents match queries. Projection can reduce data transfer but still reads full document from disk.

**Relevance**: Similar to our case - documents stored as complete units, but we want to read only specific paths.

### 2. PostgreSQL JSONB (Binary JSON + GIN Indexes)

**Storage**:
- JSONB: Binary JSON format (decomposed, normalized)
- Stored as complete documents
- Can index specific paths

**Indexing**:
- **GIN indexes**: Generalized Inverted Index
  - Can index entire JSONB column
  - Can index specific paths: `CREATE INDEX ON table USING GIN (column->'path')`
  - Supports path queries: `WHERE column->'user'->>'name' = 'John'`

**Path Access**:
- **Read**: Must read entire JSONB document
- **Query**: Uses GIN index to find matching documents
- **Extraction**: Can extract paths after reading, but full document is read from disk

**Key Insight**: GIN indexes help find documents, but full document is still read. Path extraction happens in memory after read.

**Relevance**: Similar pattern - index helps find documents, but full read is still required.

### 3. CouchDB (Document Store + Views)

**Storage**:
- Documents stored as complete JSON
- Can create views (MapReduce) that index specific fields

**Indexing**:
- **Views**: MapReduce functions that extract/index specific fields
- **Spatial indexes**: For geospatial data
- **Full-text indexes**: For text search

**Path Access**:
- Views can extract specific paths
- But documents are still stored as complete units
- **Read**: Reads entire document, views extract paths

**Key Insight**: Views help query, but documents are still read as complete units.

**Relevance**: Similar - extraction happens after read.

### 4. Elasticsearch (Inverted Indexes)

**Storage**:
- Documents stored as JSON
- **Inverted indexes**: Index all terms/values
- **Source storage**: Original document stored separately

**Indexing**:
- **Inverted index**: Maps terms → documents
- **Field indexes**: Can index specific fields
- **Nested fields**: Can index nested structures

**Path Access**:
- **Query**: Uses inverted index to find documents
- **Read**: Can retrieve only specific fields (`_source` filtering)
- **But**: Full document is still stored, filtering happens after retrieval

**Key Insight**: Inverted indexes help find documents, but source documents are still stored as complete units. Field filtering reduces network transfer but not disk I/O.

**Relevance**: Similar - helps with querying, but full documents are still read.

### 5. FoundationDB (Key-Value + Layers)

**Storage**:
- Key-value store at core
- **Layers**: Higher-level abstractions (Document Layer, etc.)
- Documents stored as key-value pairs

**Indexing**:
- **Key structure**: Can encode paths in keys
- **Range queries**: Efficient range scans
- **Layers**: Document layer provides document abstraction

**Path Access**:
- **Key encoding**: Can encode paths in keys (e.g., `doc:user:name`)
- **Partial reads**: Can read only specific keys
- **Efficient**: Only reads needed keys, not entire document

**Key Insight**: Key-value model allows partial reads - can read only specific keys/paths without reading entire document.

**Relevance**: **Very relevant** - FoundationDB's approach is similar to what we're considering (path indexing at byte offsets).

### 6. RocksDB / LevelDB (Key-Value)

**Storage**:
- Key-value store
- Keys can encode paths
- Values are arbitrary bytes

**Indexing**:
- **Key structure**: Paths encoded in keys
- **Prefix scans**: Efficient range queries
- **Bloom filters**: Fast existence checks

**Path Access**:
- **Partial reads**: Read only specific keys
- **Efficient**: Only reads needed data
- **No full document read**: Can read individual paths without reading entire document

**Key Insight**: Key-value model naturally supports partial reads - read only what you need.

**Relevance**: **Very relevant** - Similar to our path indexing approach.

### 7. Cassandra (Wide-Column)

**Storage**:
- Wide-column store
- Rows contain columns (can be sparse)
- Columns can be nested (maps, sets)

**Indexing**:
- **Primary key**: Row key + clustering columns
- **Secondary indexes**: On specific columns
- **SASI indexes**: For text search

**Path Access**:
- **Column reads**: Can read specific columns
- **Partial reads**: Don't need to read entire row
- **Efficient**: Only reads needed columns

**Key Insight**: Columnar model allows partial reads - read only needed columns.

**Relevance**: Similar concept - partial reads are possible.

## Key Patterns

### Pattern 1: Document Stores (MongoDB, CouchDB, Elasticsearch)

**Approach**:
- Store documents as complete units
- Index fields/paths for querying
- **Read**: Read entire document, extract paths in memory

**Pros**:
- Simple storage model
- Good for document-oriented workloads
- Easy to understand

**Cons**:
- Must read entire document even for single path
- Not optimal for large documents with small paths

**Relevance**: Similar to our current approach (read entire snapshot).

### Pattern 2: Key-Value Stores (FoundationDB, RocksDB)

**Approach**:
- Store paths as separate keys
- Keys encode paths (e.g., `doc:user:name`)
- **Read**: Read only specific keys/paths

**Pros**:
- Partial reads possible
- Efficient for large documents with small paths
- Natural fit for path-based access

**Cons**:
- More complex storage model
- Need to manage multiple keys per document

**Relevance**: **Very relevant** - Similar to our path indexing approach.

### Pattern 3: Columnar Stores (Cassandra)

**Approach**:
- Store columns separately
- Can read specific columns
- **Read**: Read only needed columns

**Pros**:
- Partial reads possible
- Efficient for columnar workloads

**Cons**:
- Less natural for nested JSON
- More complex for document-oriented workloads

**Relevance**: Less relevant - we're dealing with nested structures.

## Comparison to Our Approach

### Our Current Approach (Read Entire Snapshot)

**Similar to**: Document stores (MongoDB, PostgreSQL JSONB)
- Store complete snapshot
- Read entire snapshot
- Extract paths in memory

**Pros**: Simple, works for small snapshots
**Cons**: Not efficient for large snapshots with small paths

### Our Proposed Approach (Path Indexing)

**Similar to**: Key-value stores (FoundationDB, RocksDB)
- Index paths at byte offsets
- Read only specific paths
- Partial reads from snapshot

**Pros**: Efficient for large snapshots with small paths
**Cons**: More complex implementation

## Key Insights

1. **Document stores** (MongoDB, PostgreSQL) read entire documents, even when querying specific paths
2. **Key-value stores** (FoundationDB, RocksDB) support partial reads naturally
3. **Our use case** (10^6:1 ratio) is better suited to key-value approach
4. **Path indexing** is similar to key-value model - encode paths, read only what's needed

## Recommendations

**For Our Use Case** (Controller with 10^6:1 ratio):

1. **Path indexing is the right approach**
   - Similar to FoundationDB/RocksDB key-value model
   - Supports partial reads efficiently
   - Essential for large snapshots with small paths

2. **Learn from key-value stores**
   - How they encode paths in keys
   - How they handle partial reads
   - How they optimize for this pattern

3. **Consider hybrid approach**
   - Path indexing for large snapshots (> 10MB)
   - Full read for small snapshots (< 100KB)
   - Best of both worlds

## References

- MongoDB: https://docs.mongodb.com/manual/indexes/
- PostgreSQL JSONB: https://www.postgresql.org/docs/current/datatype-json.html
- FoundationDB: https://apple.github.io/foundationdb/
- RocksDB: https://rocksdb.org/
- Elasticsearch: https://www.elastic.co/guide/en/elasticsearch/reference/current/index.html
