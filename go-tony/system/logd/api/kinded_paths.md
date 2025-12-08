# Kinded Paths Design

## 1. Introduction

Kinded Paths provide a deterministic way to reference any node within a document tree. The core innovation of Kinded Paths is the ability to **reconstruct a document** purely from a set of paths and their associated values.

This capability distinguishes Kinded Paths from existing mechanisms like **JSONPath**, which are designed for *querying* existing documents. JSONPath expressions like `$.store.book[0]` are ambiguous for construction because they rely on the document already existing to resolve whether a segment refers to a map key or an array index (e.g., does `[0]` access a list or a map with key "0"?).

By explicitly encoding the container kinds into the path itself, Kinded Paths enable:
1.  **Ambiguity Resolution**: Distinguishing between Objects, Dense Arrays, and Sparse Arrays.
2.  **Document Reconstruction**: Building a complete, typed document tree from a flat list of paths.
3.  **Merge Determinism**: Merging independent updates without needing to read the current state of the store.

## 2. Grammar and Syntax

A Path is a sequence of operations starting from a Root.

### 2.1. Roots
The root of a document must be explicitly typed:
*   `{}` : Root is an empty **Sparse Array**.
*   `[]` : Root is an empty **Dense Array**.
*   `.`  : Root is an empty **Object**.

### 2.2. Path Segments
A path is composed of identifiers followed by accessors. The accessor defines the kind of the **container** being traversed.

| Container Kind | Accessor Syntax | Description |
| :--- | :--- | :--- |
| **Object** | `name.key` | Access key `key` in Object `name`. |
| **Dense Array** | `name[index]` | Access index `index` in Dense Array `name`. |
| **Sparse Array** | `name{index}` | Access index `index` in Sparse Array `name`. |

### 2.3. Quoting and Escaping (Tony Alignment)

To support arbitrary keys in Objects, we adopt the **Tony Format** string literal and quoting rules.

#### Literals (Unquoted)
Simple keys can be unquoted if they follow Tony's literal rules:
*   May contain unicode digits, letters, and graphics.
*   May NOT contain whitespace or control characters.
*   May NOT start with a digit (to avoid confusion with array indices).
*   May NOT start with: `[`, `]`, `{`, `}`, `:`, `-`, `!`.
*   Punctuation allowed: `(`, `)`, `[`, `]`, `{`, `}`, `$`, `~`, `@`, `:`, `/`, `.`, `_`, `+`, `-`, `\`, `*`, `%`, `!`, `=` (with restrictions on initial position).

**Example**: `a.key`

#### Quoted Strings
For any key that does not satisfy the literal rules (e.g., contains spaces, dots, starts with a digit), we use Tony quoting.

*   **Single Quotes** (`'...'`): Preferred.
    *   Escaping: `\'` for single quote, `\\` for backslash.
    *   Example: `a.'key with spaces'`, `a.'key.with.dots'`
*   **Double Quotes** (`"..."`): Supported.
    *   Escaping: `\"` for double quote, `\\` for backslash, standard JSON escapes (`\n`, `\t`, etc.).
    *   Example: `a."key\nwith\nnewline"`

This ensures that any valid JSON/Tony string can be used as a key in a Kinded Path.

### 2.4. Target Node (Leaf)
The final element of a path does not require a suffix/accessor. Its kind is determined by the `match` or `patch` body.
*   `a.b` : Target `b` in Object `a`.
*   `a[0]` : Target at index `0` in Dense Array `a`.
*   `a{0}` : Target at index `0` in Sparse Array `a`.

### 2.5. Chaining Examples

*   `a.b[0]`
    *   `a`: **Object** (accessed via `.b`).
    *   `b`: **Dense Array** (accessed via `[0]`).
    *   `0`: **Target**.

*   `a{0}.c`
    *   `a`: **Sparse Array** (accessed via `{0}`).
    *   `0`: **Object** (accessed via `.c`).
    *   `c`: **Target**.

*   `a.'complex.key'[0]`
    *   `a`: **Object**.
    *   `complex.key`: **Dense Array** (accessed via `[0]`).
    *   `0`: **Target**.

## 3. Filesystem Mapping

To map Kinded Paths to a filesystem, we use the **Tony string representation** of each key/index as the filesystem name, followed by a kind suffix. **Crucially, EVERY node in the filesystem (including scalar leaves) must have a suffix to avoid ambiguity.**

### 3.1. Filesystem Name Construction

For each path segment:
1.  Represent the key/index as a Tony string (literal or quoted).
2.  Append the kind suffix: `-{object,array,sparsearray,value}`.

**Quoted Detection**: A filesystem name is considered to represent a quoted key if its first character is `"` or `'`.

### 3.2. Directory/File Suffixes

*   **Object** (Directory): `TonyString-object/`
*   **Dense Array** (Directory): `TonyString-array/`
*   **Sparse Array** (Directory): `TonyString-sparsearray/`
*   **Scalar Value** (File): `TonyString-value`

Where `TonyString` is the Tony representation of the key/index (including quotes if needed).

### 3.3. Ambiguity Resolution
By enforcing suffixes on all nodes, we resolve collisions between keys that happen to end in a suffix string.

**Example Collision Scenario**:
*   Key `data` is an Array.
*   Key `data-array` is a Scalar.

**Mapping**:
1.  `data` (Array) -> `data-array/` (Directory)
2.  `data-array` (Scalar) -> `data-array-value` (File)

Since `data-array` (Dir) != `data-array-value` (File), there is no collision.

### 3.4. Mapping Examples

| Kinded Path | Target Kind | Tony Representation | Filesystem Path |
| :--- | :--- | :--- | :--- |
| `a.b` | Scalar | `b` | `a-object/b-value` |
| `a.b` | Object | `b` | `a-object/b-object/` |
| `a.'b.c'` | Scalar | `'b.c'` | `a-object/'b.c'-value` |
| `a."key\nline"` | Scalar | `"key\nline"` | `a-object/"key\nline"-value` |
| `a[0]` | Scalar | `0` | `a-array/0-value` |
| `a{0}` | Object | `0` | `a-sparsearray/0-object/` |
| `data` | Array | `data` | `data-array/` |
| `data-array` | Scalar | `data-array` | `data-array-value` |

## 4. Summary
This design provides a clear, unambiguous syntax for traversing three distinct container types:
1.  **Objects**: `.`
2.  **Dense Arrays**: `[]`
3.  **Sparse Arrays**: `{}`

The filesystem mapping uses the Tony string representation (including quotes) as the base filename, followed by mandatory suffixes (`-object`, `-array`, `-sparsearray`, `-value`) for all nodes. This ensures that the filesystem structure is isomorphic to the document structure, free of name collisions, and naturally handles all escaping through Tony's string representation.
