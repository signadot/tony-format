# logd: a write and every watcher fold the whole document, once each, per commit

Item 3 of ntadpaech12krandgsn0, filed on its own -- and corrected, because the TODO that
issue quoted was on dead code.

WHAT IS NOT THE PROBLEM. patches/applier.go carried "TODO: Replace with streaming
implementation that never materializes full document". Nothing had called NewInMemoryApplier
since StreamingProcessor landed; the note outlived its own replacement and was still being
cited as the state of the code. The file is deleted. The live read path streams the base
events and materializes only the subtrees a patch reaches.

WHAT IS. Two places fold a WHOLE document, and both run on every commit.

1. The commit path. verifyApplies folds the patch onto the entire baseline head to check it
   applies, and keeps the result as the next head. Measured today, small one-path writes,
   set size varying:

       200 entities    apply  38µs   of a 120µs commit
       1000 entities   apply  78µs   of a 148µs commit
       3000 entities   apply 137µs   of a 186µs commit

   Apply is the leading term again and it scales with the set, not the patch. The merge
   itself is no longer the cost (objMergeFast, v0.0.168): what remains is that the fold is
   asked about the whole document when the patch touches one path.

2. Every baseline watcher. stepBaseline folds the same committed delta onto its own copy of
   the whole document, per commit, per watcher. A store with W watchers pays W times the
   above on every commit, and a verse-shaped deployment has dozens. This is already cheaper
   than what it replaced (a full ReadStateAt per event per watcher) but it is the same shape:
   O(document) for a delta that names one path.

WHY IT IS TRACTABLE NOW, which it was not when the TODO was written:

  - the snapshot's path index seeks, so a subtree can be read without the rest
    (narrowSubtreeAt);
  - a segment says whether a patch merely passed through a path or landed there (Spine,
    v0.0.170), so the paths a patch actually writes are known from the index;
  - the object merge walks two ordered field lists instead of rebuilding through a map
    (v0.0.168), so a subtree fold is genuinely proportional to the subtree.

SHAPE OF A FIX. The head does not have to be one document. A patch names its paths; the
check and the step both want the state AT those paths. Keeping the head as the subtrees that
have been read or written -- and folding only those -- turns both costs into O(patch),
leaving a whole-document read for the cases that genuinely want one (a root read, a
snapshot). The watch path wants the same treatment and gets it for free if the fold does:
a watcher holds the subtree it watches, not the document it is a leaf of.

The bound to hold it to: a one-path write, and a one-path watch event, must cost the same at
3000 entities as at 200.