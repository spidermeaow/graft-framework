# 003: Explicit OpenAPI, bundled Swagger UI

Route options describe operations and explicit schemas. No reflection, comment
scanner, code generation or runtime schema validation. Generate OpenAPI 3.0.3
with encoding/json. Discover path parameters as required strings, with optional
explicit overrides. Metadata is snapshotted on registration. Docs are opt-in;
generated apps enable them. ServeMux multi-segment wildcards return a clear
generation error rather than inventing an incompatible OpenAPI path syntax.

Vendor Swagger UI's pinned Apache-2.0 distribution and embed only its browser
bundle, CSS, licenses and a local initializer. No CDN, npm, Node or remote
validator is needed at runtime. This adds roughly 2 MB of static assets to the
core binary; no third-party Go dependency is required for documentation.
Security scheme configuration and advanced schema composition are future scope.
