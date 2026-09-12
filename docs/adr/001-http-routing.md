# 001: Standard-library HTTP core

Use Go 1.26 and `http.ServeMux` method/path patterns. Graft implements
`http.Handler`; middleware uses `func(http.Handler) http.Handler`. Registration
ends when serving starts. There is no application container or global app.

Handlers return errors; the default error handler exposes only explicit HTTP
errors. Recovery is installed around routing by default, so application request
logging observes recovered handler errors. An outer recovery also protects
middleware panics. Request IDs and slog request logging are explicit.

Keep ServeMux's HEAD, 404, 405, Allow, redirect and path-cleaning semantics.
Streaming capabilities remain accessible through `http.ResponseController`.
The built-in server supplies timeouts and bounded graceful shutdown; advanced
applications own an `http.Server` and use the same App as its handler.
