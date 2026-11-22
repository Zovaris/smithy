`Check` never rejects anything. Make it return the error `no such path: <p>` for the first path
that is neither on disk nor tracked by git; a path that is missing from disk but still tracked
is fine. Add a test.
