# WarmPoolSpecTemplateSpecVolumesInnerFc


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**fs_type** | **str** |  | [optional] 
**lun** | **int** |  | [optional] 
**read_only** | **bool** |  | [optional] 
**target_wwns** | **List[str]** |  | [optional] 
**wwids** | **List[str]** |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_volumes_inner_fc import WarmPoolSpecTemplateSpecVolumesInnerFc

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerFc from a JSON string
warm_pool_spec_template_spec_volumes_inner_fc_instance = WarmPoolSpecTemplateSpecVolumesInnerFc.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecVolumesInnerFc.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_volumes_inner_fc_dict = warm_pool_spec_template_spec_volumes_inner_fc_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerFc from a dict
warm_pool_spec_template_spec_volumes_inner_fc_from_dict = WarmPoolSpecTemplateSpecVolumesInnerFc.from_dict(warm_pool_spec_template_spec_volumes_inner_fc_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


