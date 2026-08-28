# Reconciliation

1С-Битрикс remote product state is authoritative. Before a write the connector
fetches by remote ID or exact `xmlId`, compares the bounded product fields, and
returns a duplicate receipt when the requested state already exists. After a
successful add/update it re-fetches and verifies the response. Timeout,
unavailable and other ambiguous transport failures trigger a read-back
reconciliation; the connector never blindly repeats a potentially applied
write.

