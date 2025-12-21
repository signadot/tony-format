# Logd Transactions

##  Operational Context

Logd is a backend to an architecture that supports a transactional diff store of
a virtual document

```
      ----- Controller -----
     /                      \
docd ----------------------Logd
     \                      /
      ----- Controller -----
```

The frontend, docd, is what clients hit with requests.

The controllers mount the virtual document at different mount points,
like a filesystem, providing virtual support for a document.

docd partitions the incoming requests, sending each part to a controller.

When a request makes changes to the virtual document, and that
document is updated in several different controllers, docd uses
a _transaction_.

This notion of a transaction is weaker than say a typical SQL server
because it happens in the scope of a single interaction with docd rather
than being chained across operations.

However, the updates via docd still need to be coordinated to keep the
virtual document coherent whilst the controllers operate independently. Logd
transactions serve to guarantee that coordinated updates by controllers either
all succeed or all fail: no reader of logd will see a view that only partially
applies the writes from the controllers.

### Requests

Requests can contain matches and patches.  Whenever there is a patch, there
is an update.  Whenever logd partitions a request to more than 1 controller, a
transaction is needed to coordinate.

## Docd, Controller and Logd Interactions

1. Docd requests a transaction id from logd for N participants.
1. Docd sends the partitioned request to each controller involved along
with the tx id. 
1. Each controller does some computation to custom-handle the request
and uses logd for storage.  For example, it may add computed fields to
the objects it manages.
1. Whenever a controller writes the computed object to storage, it requests
a patch to logd with the associated tx id.  the controller waits synchronously
for a response.  A controller may associate a max duration with its request.
1. logd collects requests by tx-id, and when N requests are received and
persisted, it peforms the transaction.

> Note that in this context there is no risk over participants overwriting each
other by design because they have been partitioned by Docd.

## How Logd manages transactions.

Logd simply keeps a store where each transaction lives in a single file
containing information from each participant.

```
tx: 03230
participants: N
meta: # docd metadata for the original request
participants: # participants partitioned by path
 some/virtual/document/path: # 1 particpant
   receivedAt:
   maxDuration:
   match:
   patch:
 some/other/path: ... # another participant
   receivedAt:
   maxDuration:
   match:
   patch:
```

Once all partipants have recorded their info, logd will run the merged patch
like any other not in a transaction.  It just needs to make sure that timing
constraints are respected prior to commiting.

### merging participants

In order to merge participants, logd needs to find find the most specific common
ancestor between the components and produce a diff associated with that common
ancestor as root.

Doing this in turn requires knowing the kind (object, array, sparsearray) of
each node at the tip of the transaction as well as the kind of each intermediary
node in the diff.



