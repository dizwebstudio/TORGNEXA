(
  [.Results[]?.Secrets[]? | select(.Classification != "synthetic_fixture")]
  | length == 0
)
and
(
  [.Results[]?.Secrets[]?]
  | all(
      .Classification == "synthetic_fixture" and
      (.FixtureID | type == "string" and length > 0) and
      (has("Match") | not) and
      (has("Secret") | not) and
      (has("Code") | not)
    )
)
