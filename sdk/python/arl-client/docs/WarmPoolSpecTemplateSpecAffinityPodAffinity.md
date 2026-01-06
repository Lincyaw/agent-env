# WarmPoolSpecTemplateSpecAffinityPodAffinity


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**preferred_during_scheduling_ignored_during_execution** | [**List[WarmPoolSpecTemplateSpecAffinityPodAffinityPreferredDuringSchedulingIgnoredDuringExecutionInner]**](WarmPoolSpecTemplateSpecAffinityPodAffinityPreferredDuringSchedulingIgnoredDuringExecutionInner.md) |  | [optional] 
**required_during_scheduling_ignored_during_execution** | [**List[WarmPoolSpecTemplateSpecAffinityPodAffinityPreferredDuringSchedulingIgnoredDuringExecutionInnerPodAffinityTerm]**](WarmPoolSpecTemplateSpecAffinityPodAffinityPreferredDuringSchedulingIgnoredDuringExecutionInnerPodAffinityTerm.md) |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_affinity_pod_affinity import WarmPoolSpecTemplateSpecAffinityPodAffinity

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecAffinityPodAffinity from a JSON string
warm_pool_spec_template_spec_affinity_pod_affinity_instance = WarmPoolSpecTemplateSpecAffinityPodAffinity.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecAffinityPodAffinity.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_affinity_pod_affinity_dict = warm_pool_spec_template_spec_affinity_pod_affinity_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecAffinityPodAffinity from a dict
warm_pool_spec_template_spec_affinity_pod_affinity_from_dict = WarmPoolSpecTemplateSpecAffinityPodAffinity.from_dict(warm_pool_spec_template_spec_affinity_pod_affinity_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


