##### Upstream sync helpers (ovh/venom <-> socotra/venom) #####
# See docs/UPSTREAM.md

UPSTREAM_SCRIPT := ./scripts/upstream.sh

.PHONY: upstream-status upstream-sync upstream-feature upstream-check

upstream-status: ## Fetch remotes; show master / internal/develop vs upstream
	@$(UPSTREAM_SCRIPT) status

upstream-sync: ## Fast-forward master; create sync/upstream-DATE from internal/develop
	@$(UPSTREAM_SCRIPT) sync $(if $(PUSH),--push,)

upstream-feature: ## Create upstream/NAME from master (NAME=... required)
	@if [ -z "$(NAME)" ]; then echo "usage: make upstream-feature NAME=my-change"; exit 1; fi
	@$(UPSTREAM_SCRIPT) feature $(NAME)

upstream-check: ## Warn if current (or BRANCH=) branch touches fork-only paths
	@$(UPSTREAM_SCRIPT) check $(BRANCH)
