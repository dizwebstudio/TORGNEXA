# LM Studio conformance plan

The canonical Task-064 candidate (`connectors/lm-studio/conformance.go`) uses a
deterministic fixture transport and never contacts a desktop LM Studio
process. Its retained report covers all thirteen SDK checks, including sandbox
isolation. Package tests cover the configured endpoint, authorization header
and OpenAI-compatible response mapping.
