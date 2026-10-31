# stream: a second tag on one value is refused, and Reset drops a pending tag

The wire decoder holds a tag in pendingTag until its value arrives, and two defects follow from that.

**A second tag overwrites the first.** `[!t !u]` decodes as `[!u null]` and `[!t !u x]` as `[!u x]`. The parser refuses both: "two tags on one node, !t and !u: tags compose with '.', as !t.u".

**Reset leaves pendingTag set.** A tag left unread at the end of one stream goes on the first value of the next stream.

Fix:
- refuse a second tag with the parser's message
- clear pendingTag in Reset

Found in review of branch issue-v5dn9vfbh12krj.