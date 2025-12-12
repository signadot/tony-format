  !snap-array [
    {
      begin:
      end:
      offset:
      size:
    }
    {
      begin:
      end:
      offset:
      size:
    }
	]

  !snap-object [
    {
      fieldStart: "a-field"
      [
        [
          {
            begin: 0
            end: 100
          }
        ]
        offset: 10938
        size:  6402
      ]
    }
    {
      fieldStart: "b-field"
      begin:
      end:
      offset:
      size:
    }
  
    
  ]

There are 3 kinds of snap Nodes

  - interieur nodes which may be too big to put in memory.  These have child nodes
    and are not tagged with !snap-chunks.
    
  - snap-loc leaf nodes. These represent whole objects which can be read into
    memory as is, located at the offset corresponding to the first Int64 value of the ir.Node.
    and of size corresponding to the second value.

  - chunked nodes.  These are nodes representing objects or (sparse) arrays which have
    to be reconstructed piece-meal to stay representable in-memory.

A Tree of snap Nodes represents the index, and it is assumed the entire index can be
read into memory.
package snap
