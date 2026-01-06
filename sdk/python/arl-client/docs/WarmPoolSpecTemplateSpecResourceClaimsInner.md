# WarmPoolSpecTemplateSpecResourceClaimsInner


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**name** | **str** |  | 
**resource_claim_name** | **str** |  | [optional] 
**resource_claim_template_name** | **str** |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_resource_claims_inner import WarmPoolSpecTemplateSpecResourceClaimsInner

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecResourceClaimsInner from a JSON string
warm_pool_spec_template_spec_resource_claims_inner_instance = WarmPoolSpecTemplateSpecResourceClaimsInner.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecResourceClaimsInner.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_resource_claims_inner_dict = warm_pool_spec_template_spec_resource_claims_inner_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecResourceClaimsInner from a dict
warm_pool_spec_template_spec_resource_claims_inner_from_dict = WarmPoolSpecTemplateSpecResourceClaimsInner.from_dict(warm_pool_spec_template_spec_resource_claims_inner_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


