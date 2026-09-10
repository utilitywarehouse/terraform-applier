# Integration-test policy bundle

## Running locally

Generate a plan for any module, then pipe its JSON to conftest. The applier
always evaluates both tiers against the same plan, so run them the same way:

```sh
# from an initialized terraform module that has a saved plan
terraform show -json plan.out > plan.json

# hard tier (unconditional denials)
conftest test --policy integration_test/src/policies/hard \
  --data integration_test/src/policies/data \
  --namespace main --output json - < plan.json

# soft tier (override-required denials)
conftest test --policy integration_test/src/policies/soft \
  --data integration_test/src/policies/data \
  --namespace main --output json - < plan.json

# or both tiers in a single invocation
conftest test --policy integration_test/src/policies/hard \
  --policy integration_test/src/policies/soft \
  --data integration_test/src/policies/data \
  --namespace main --output json - < plan.json
```
