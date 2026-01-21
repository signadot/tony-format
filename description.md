# codegen zero values again

Found another bug. The helper function generation (line 376) uses getTypeString(field.Type) but should use
  getFieldTypeName(field, currentPkg):

  // Line 376 - current code:
  typeStr := getTypeString(field.Type)

  // Should be:
  typeStr := getFieldTypeName(field, currentPkg)

  The getFieldTypeName function (line 1566) exists and correctly prefers stored TypePkgPath/TypeName over reflection, but it's
   not being used in the helper function generation.