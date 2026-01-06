# WarmPoolSpecTemplateSpecAffinityNodeAffinity


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**preferred_during_scheduling_ignored_during_execution** | [**List[WarmPoolSpecTemplateSpecAffinityNodeAffinityPreferredDuringSchedulingIgnoredDuringExecutionInner]**](WarmPoolSpecTemplateSpecAffinityNodeAffinityPreferredDuringSchedulingIgnoredDuringExecutionInner.md) |  | [optional] 
**required_during_scheduling_ignored_during_execution** | [**WarmPoolSpecTemplateSpecAffinityNodeAffinityRequiredDuringSchedulingIgnoredDuringExecution**](WarmPoolSpecTemplateSpecAffinityNodeAffinityRequiredDuringSchedulingIgnoredDuringExecution.md) |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_affinity_node_affinity import WarmPoolSpecTemplateSpecAffinityNodeAffinity

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecAffinityNodeAffinity from a JSON string
warm_pool_spec_template_spec_affinity_node_affinity_instance = WarmPoolSpecTemplateSpecAffinityNodeAffinity.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecAffinityNodeAffinity.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_affinity_node_affinity_dict = warm_pool_spec_template_spec_affinity_node_affinity_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecAffinityNodeAffinity from a dict
warm_pool_spec_template_spec_affinity_node_affinity_from_dict = WarmPoolSpecTemplateSpecAffinityNodeAffinity.from_dict(warm_pool_spec_template_spec_affinity_node_affinity_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


