# WarmPoolSpecTemplateSpecAffinity


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**node_affinity** | [**WarmPoolSpecTemplateSpecAffinityNodeAffinity**](WarmPoolSpecTemplateSpecAffinityNodeAffinity.md) |  | [optional] 
**pod_affinity** | [**WarmPoolSpecTemplateSpecAffinityPodAffinity**](WarmPoolSpecTemplateSpecAffinityPodAffinity.md) |  | [optional] 
**pod_anti_affinity** | [**WarmPoolSpecTemplateSpecAffinityPodAffinity**](WarmPoolSpecTemplateSpecAffinityPodAffinity.md) |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_affinity import WarmPoolSpecTemplateSpecAffinity

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecAffinity from a JSON string
warm_pool_spec_template_spec_affinity_instance = WarmPoolSpecTemplateSpecAffinity.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecAffinity.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_affinity_dict = warm_pool_spec_template_spec_affinity_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecAffinity from a dict
warm_pool_spec_template_spec_affinity_from_dict = WarmPoolSpecTemplateSpecAffinity.from_dict(warm_pool_spec_template_spec_affinity_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


