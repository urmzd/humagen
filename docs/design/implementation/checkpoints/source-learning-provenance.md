# Typed Learning Dependencies

The upper native source dependency reader lists skills and reviewed decision
memory derived from actual retrieval runs. It composes the Source layer's
`SourceDependencyReader` seam with the learning stores. Ordinary source, run,
scope and owner access are required. Governance alone grants no copy authority.

- Skill edges use the server-written `Revision.OriginRun` only.
- Reviewed memory origins are private metadata in the same durable owner record
  as the note. The note's human author and reviewer remain unchanged. Origin run
  IDs are absent from note JSON and dependency DTOs.
- Failed writes cannot leave a note or origin partially committed. Restart
  preserves the relation. Ordinary note creation, duplicate retries and
  promotion cannot add, replace or copy an origin. Legacy notes are not inferred.
- The reader accepts at most 200 proven run IDs and returns at most 50 rows of
  each kind. It uses canonical Library links and no source-name or text matching.
- Explicit configured readers are preserved; the native adapter is the default
  only when none was configured.

Evidence: `TestSourceDependenciesUseOnlyVisibleTypedLearningOrigins` failed on
memory and Postgres before native registration, returning unsupported skills and
memory despite real learning writes. With the adapter it passes, including
restart, unrelated sources, hidden origin metadata, ordinary membership and
permission revocation, and governance-only denial.
`TestOutcomeSourceDependenciesBoundInputsAndVisibleRows` checks input and output
bounds, cancellation and machine denial.
`TestReviewedOriginIsAtomicPrivateAndNeverInferredOrCopied` checks failed writes,
restart, JSON privacy, human attribution, duplicate retries, legacy notes,
promotion isolation and deletion.

This checkpoint changes no UI components and makes no new screenshot claim.
