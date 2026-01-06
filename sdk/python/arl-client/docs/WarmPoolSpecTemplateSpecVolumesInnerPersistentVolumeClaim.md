# WarmPoolSpecTemplateSpecVolumesInnerPersistentVolumeClaim


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**claim_name** | **str** |  | 
**read_only** | **bool** |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_volumes_inner_persistent_volume_claim import WarmPoolSpecTemplateSpecVolumesInnerPersistentVolumeClaim

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerPersistentVolumeClaim from a JSON string
warm_pool_spec_template_spec_volumes_inner_persistent_volume_claim_instance = WarmPoolSpecTemplateSpecVolumesInnerPersistentVolumeClaim.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecVolumesInnerPersistentVolumeClaim.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_volumes_inner_persistent_volume_claim_dict = warm_pool_spec_template_spec_volumes_inner_persistent_volume_claim_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerPersistentVolumeClaim from a dict
warm_pool_spec_template_spec_volumes_inner_persistent_volume_claim_from_dict = WarmPoolSpecTemplateSpecVolumesInnerPersistentVolumeClaim.from_dict(warm_pool_spec_template_spec_volumes_inner_persistent_volume_claim_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


