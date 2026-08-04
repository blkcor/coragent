# Discovery

Mercury skips the root `.tmp/` tree, every directory named `vendor`, and hidden
directories nested below another visible directory. A hidden directory directly
under the discovery root remains visible. Directory exclusion runs before file
extension matching. Extension matching is currently case-sensitive.
