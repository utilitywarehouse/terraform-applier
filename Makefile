BJS_VERSION="5.1.0"
update-bootstrap-js:
	(cd /tmp/ && curl -L -O https://github.com/twbs/bootstrap/releases/download/v$(BJS_VERSION)/bootstrap-$(BJS_VERSION)-dist.zip)
	(cd /tmp/ && unzip bootstrap-$(BJS_VERSION)-dist.zip)
	cp /tmp/bootstrap-$(BJS_VERSION)-dist/js/bootstrap.js static/bootstrap/js/bootstrap.js

update-jquery-js:
	curl -o static/bootstrap/js/jquery.min.js https://code.jquery.com/jquery-3.6.0.min.js

release:
	@sd "latest" "$(VERSION)" manifests/base/terraform-applier.yaml
	@git add -- manifests/base/terraform-applier.yaml
	@git commit -m "Release $(VERSION)"
	@sd "$(VERSION)" "latest" manifests/base/terraform-applier.yaml
	@git add -- manifests/base/terraform-applier.yaml
	@git commit -m "Clean up release $(VERSION)"
