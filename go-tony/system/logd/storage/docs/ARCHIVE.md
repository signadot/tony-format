# Archived Documents

This directory contains archived documents from the design process. These documents are kept for historical reference but should not be used as the primary design reference.

## Primary Reference

**Use `DESIGN.md` for the final design reference.**

## Archived Documents

The following documents contain the decision-making process but are archived:

### Critique Documents
- `log_entry_critique.md` - Critique of LogEntry schema
- `write_timing_critique.md` - Analysis of write timing approaches
- `index_critique.md` - Analysis of index persistence
- `parent_diff_indexing.md` - Analysis of parent diff storage
- `extract_key_vs_path.md` - ExtractKey vs ExtractPath analysis
- `length_prefix_analysis.md` - Length-prefixed format analysis
- `remove_path_field.md` - Why remove Path field
- `read_path_sanity_check.md` - Read operation sanity check
- `bottom_up_critique.md` - Bottom-up component analysis

### Analysis Documents
- `parent_diff_write_contention.md` - Write contention analysis
- `single_entry_per_path.md` - Why merge to root
- `merge_to_root.md` - Analysis of merging to root

### Superseded Documents
- `redesign_outline.md` - Original design outline (superseded by DESIGN.md)
- `redesign_summary.md` - Original summary (superseded by DESIGN.md)
- `final_design.md` - Earlier final design attempt (superseded by DESIGN.md)
- `design_alternatives.md` - Design alternatives considered
- `design_justification.md` - Design justifications

### Other Documents
- `compaction_redesign.md` - Compaction redesign (may be relevant)
- `compaction_filtering_analysis.md` - Compaction filtering analysis
- `alignment_clarification.md` - Alignment clarification
- `intent_based_compaction.md` - Intent-based compaction design
- `intent_with_paths.md` - Intent with paths
- `implementation_plan_intent_based.md` - Intent-based implementation plan
- `lock_analysis.md` - Lock analysis
- `parallelization_strategy.md` - Parallelization strategy
- `tx.md` - Transaction design
- `deletable_code.md` - Code that can be deleted

## Notes

- All final design decisions are documented in `DESIGN.md`
- Historical decision-making process is archived in `design_decisions_archive.md`
- Use `DESIGN.md` as the source of truth for implementation
