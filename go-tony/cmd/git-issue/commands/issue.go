package commands

import (
	"fmt"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
)

// Issue represents an issue's metadata
type Issue struct {
	ID           int64
	Status       string // "open" or "closed"
	Created      time.Time
	Updated      time.Time
	Commits      []string
	Branches     []string
	ClosedBy     *string
	Title        string // From description.md first line
	RelatedIssues []string
	Blocks       []string
	BlockedBy    []string
	Duplicates   []string
}

// MetaToNode converts Issue metadata to ir.Node (tony format)
func (i *Issue) MetaToNode() *ir.Node {
	fields := map[string]*ir.Node{
		"id":      ir.FromInt(i.ID),
		"status":  ir.FromString(i.Status),
		"created": ir.FromString(i.Created.Format(time.RFC3339)),
		"updated": ir.FromString(i.Updated.Format(time.RFC3339)),
	}

	// Add commits array if non-empty
	if len(i.Commits) > 0 {
		commits := make([]*ir.Node, len(i.Commits))
		for j, c := range i.Commits {
			commits[j] = ir.FromString(c)
		}
		fields["commits"] = ir.FromSlice(commits)
	} else {
		fields["commits"] = ir.FromSlice([]*ir.Node{})
	}

	// Add branches array if non-empty
	if len(i.Branches) > 0 {
		branches := make([]*ir.Node, len(i.Branches))
		for j, b := range i.Branches {
			branches[j] = ir.FromString(b)
		}
		fields["branches"] = ir.FromSlice(branches)
	} else {
		fields["branches"] = ir.FromSlice([]*ir.Node{})
	}

	// Add closed_by if present
	if i.ClosedBy != nil {
		fields["closed_by"] = ir.FromString(*i.ClosedBy)
	} else {
		fields["closed_by"] = &ir.Node{Type: ir.NullType}
	}

	// Add related issues arrays
	if len(i.RelatedIssues) > 0 {
		related := make([]*ir.Node, len(i.RelatedIssues))
		for j, r := range i.RelatedIssues {
			related[j] = ir.FromString(r)
		}
		fields["related_issues"] = ir.FromSlice(related)
	} else {
		fields["related_issues"] = ir.FromSlice([]*ir.Node{})
	}

	if len(i.Blocks) > 0 {
		blocks := make([]*ir.Node, len(i.Blocks))
		for j, b := range i.Blocks {
			blocks[j] = ir.FromString(b)
		}
		fields["blocks"] = ir.FromSlice(blocks)
	} else {
		fields["blocks"] = ir.FromSlice([]*ir.Node{})
	}

	if len(i.BlockedBy) > 0 {
		blockedBy := make([]*ir.Node, len(i.BlockedBy))
		for j, b := range i.BlockedBy {
			blockedBy[j] = ir.FromString(b)
		}
		fields["blocked_by"] = ir.FromSlice(blockedBy)
	} else {
		fields["blocked_by"] = ir.FromSlice([]*ir.Node{})
	}

	if len(i.Duplicates) > 0 {
		duplicates := make([]*ir.Node, len(i.Duplicates))
		for j, d := range i.Duplicates {
			duplicates[j] = ir.FromString(d)
		}
		fields["duplicates"] = ir.FromSlice(duplicates)
	} else {
		fields["duplicates"] = ir.FromSlice([]*ir.Node{})
	}

	return ir.FromMap(fields)
}

// NodeToMeta converts ir.Node (tony format) to Issue metadata
func NodeToMeta(node *ir.Node) (*Issue, error) {
	if node.Type != ir.ObjectType {
		return nil, fmt.Errorf("expected object, got %v", node.Type)
	}

	issue := &Issue{}

	// Parse fields
	for i, field := range node.Fields {
		val := node.Values[i]
		switch field.String {
		case "id":
			if val.Type == ir.NumberType && val.Int64 != nil {
				issue.ID = *val.Int64
			}
		case "status":
			if val.Type == ir.StringType {
				issue.Status = val.String
			}
		case "created":
			if val.Type == ir.StringType {
				t, err := time.Parse(time.RFC3339, val.String)
				if err == nil {
					issue.Created = t
				}
			}
		case "updated":
			if val.Type == ir.StringType {
				t, err := time.Parse(time.RFC3339, val.String)
				if err == nil {
					issue.Updated = t
				}
			}
		case "commits":
			if val.Type == ir.ArrayType {
				issue.Commits = make([]string, len(val.Values))
				for j, v := range val.Values {
					if v.Type == ir.StringType {
						issue.Commits[j] = v.String
					}
				}
			}
		case "branches":
			if val.Type == ir.ArrayType {
				issue.Branches = make([]string, len(val.Values))
				for j, v := range val.Values {
					if v.Type == ir.StringType {
						issue.Branches[j] = v.String
					}
				}
			}
		case "closed_by":
			if val.Type == ir.StringType {
				s := val.String
				issue.ClosedBy = &s
			}
		case "related_issues":
			if val.Type == ir.ArrayType {
				issue.RelatedIssues = make([]string, len(val.Values))
				for j, v := range val.Values {
					if v.Type == ir.StringType {
						issue.RelatedIssues[j] = v.String
					}
				}
			}
		case "blocks":
			if val.Type == ir.ArrayType {
				issue.Blocks = make([]string, len(val.Values))
				for j, v := range val.Values {
					if v.Type == ir.StringType {
						issue.Blocks[j] = v.String
					}
				}
			}
		case "blocked_by":
			if val.Type == ir.ArrayType {
				issue.BlockedBy = make([]string, len(val.Values))
				for j, v := range val.Values {
					if v.Type == ir.StringType {
						issue.BlockedBy[j] = v.String
					}
				}
			}
		case "duplicates":
			if val.Type == ir.ArrayType {
				issue.Duplicates = make([]string, len(val.Values))
				for j, v := range val.Values {
					if v.Type == ir.StringType {
						issue.Duplicates[j] = v.String
					}
				}
			}
		}
	}

	return issue, nil
}
