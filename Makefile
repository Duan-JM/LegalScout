BREW_DEV_TAP := duan-jm/legalscout-dev
BREW_DEV_FORMULA := $(BREW_DEV_TAP)/legalscout-dev

.PHONY: test vet build brew-install-dev brew-reinstall-dev brew-uninstall-dev

test:
	go test ./...

vet:
	go vet ./...

build:
	go build ./cmd/legalscout

brew-install-dev:
	brew tap --custom-remote $(BREW_DEV_TAP) "$(CURDIR)"
	git -C "$$(brew --repo $(BREW_DEV_TAP))" pull --ff-only
	brew install --HEAD $(BREW_DEV_FORMULA)

brew-reinstall-dev:
	brew tap --custom-remote $(BREW_DEV_TAP) "$(CURDIR)"
	git -C "$$(brew --repo $(BREW_DEV_TAP))" pull --ff-only
	brew reinstall $(BREW_DEV_FORMULA)

brew-uninstall-dev:
	brew uninstall legalscout-dev
	brew untap $(BREW_DEV_TAP)
