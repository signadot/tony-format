# docd: a patch spanning mounts inside a client transaction committed outside it

Found while documenting transactions, which Scott asked to note "won't work across mount
boundaries". Two different cases, and only one of them was a documentation matter.

WHAT WORKS. A client-driven transaction whose participants each lie within one mount:

    newtx(2) -> docd serves txId 11 from its pool (participants=2)
    patchTx verse.a.x  -> routed to ctrlA, joins tx 11 on logd
    patchTx verse.b.y  -> routed to ctrlB, joins tx 11 on logd
    both report commit 1

Mounts share the commit sequence, and each controller joins the client's transaction on the
one logd. Nothing special is needed.

WHAT DID NOT, silently. A participant patch which itself SPANS mounts. docd decomposes such
a patch into one participant per mount -- and coordinatePatch allocated its OWN transaction
id, ignoring the client's:

    newtx(2) -> tx 11
    participant 1: patchTx "verse" {a: {x: 1}, b: {y: 2}}   -> committed at commit 1,
                                                               in a transaction of docd's
    participant 2: patchTx "other.z" {n: 3}                 -> "not all participants joined
                                                               within 1s"

So a write the client was told was atomic with another landed alone, and the other was
refused. That is the worst available outcome: not an error, a lie.

WHY IT CANNOT SIMPLY BE HONOURED. A transaction's participant count is fixed when it is
created, by the client, which counted its own patches -- it cannot have counted docd's
decomposition of one of them into several. docd cannot join N times for one client
participant, and it cannot widen a transaction after the fact.

FIXED BY REFUSING, with a message that says what to do instead:

    invalid_tx: a patch inside a transaction may not span mounts: "verse" covers
    [verse.a verse.b] and the base; send one patch per mount as its own participant,
    and count them in newtx

A stand-alone spanning patch (no txId) is unaffected: docd decomposes it into its own
transaction, which is what that mechanism is for.