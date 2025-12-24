# walkthroughs of possible RPC style procedure call in tony-api

Let's create a few walkthroughs of how one would implement RPC
style procedures on tonyapi.

This needs discussion b/c the model is "1 giant virtual document",
and RPCs don't immediately map to that b/c they have distinct input and
output rather than backed state per se.