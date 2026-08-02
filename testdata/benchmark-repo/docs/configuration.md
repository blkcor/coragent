# Configuration

Mercury resolves configuration from lowest to highest precedence:

1. built-in defaults;
2. the user configuration file;
3. the project configuration file;
4. `MERCURY_*` environment variables;
5. command-line flags.

The default `queue_size` is 10 and the default `retries` value is 3. Mercury
loads every layer first, then validates the effective configuration. Queue size
must be positive, retries must not be negative, and configured extensions must
start with a dot.
