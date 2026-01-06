# WarmPoolSpecTemplateSpecVolumesInnerDownwardAPI


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**default_mode** | **int** |  | [optional] 
**items** | [**List[WarmPoolSpecTemplateSpecVolumesInnerDownwardAPIItemsInner]**](WarmPoolSpecTemplateSpecVolumesInnerDownwardAPIItemsInner.md) |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_volumes_inner_downward_api import WarmPoolSpecTemplateSpecVolumesInnerDownwardAPI

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerDownwardAPI from a JSON string
warm_pool_spec_template_spec_volumes_inner_downward_api_instance = WarmPoolSpecTemplateSpecVolumesInnerDownwardAPI.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecVolumesInnerDownwardAPI.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_volumes_inner_downward_api_dict = warm_pool_spec_template_spec_volumes_inner_downward_api_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerDownwardAPI from a dict
warm_pool_spec_template_spec_volumes_inner_downward_api_from_dict = WarmPoolSpecTemplateSpecVolumesInnerDownwardAPI.from_dict(warm_pool_spec_template_spec_volumes_inner_downward_api_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


