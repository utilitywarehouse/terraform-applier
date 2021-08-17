release:
	@sd "latest" "$(VERSION)" manifests/base/terraform-applier.yaml
	@git add -- manifests/base/terraform-applier.yaml
	@git commit -m "Release $(VERSION)"
	@sd "$(VERSION)" "latest" manifests/base/terraform-applier.yaml
	@git add -- manifests/base/terraform-applier.yaml
	@git commit -m "Clean up release $(VERSION)"
