# logd: baseline write overrides a scope's prior write (COW isolation bug)

logd COW scope isolation is broken: a later baseline write overrides a value the scope already wrote.