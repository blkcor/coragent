# Worker execution

Worker commands have a fixed 30-second timeout. Cancellation owns the full
process group so descendants do not survive the worker command.
