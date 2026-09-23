package ingest

// SchemaRevision identifies the import rule set. Every parsed layer carries
// it; changing parsing rules bumps this and keeps old parse results
// distinguishable during replay.
const SchemaRevision = "import-rules-1.1.0"
