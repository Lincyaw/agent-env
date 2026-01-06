# WarmPoolSpecTemplateSpecAffinityPodAffinityPreferredDuringSchedulingIgnoredDuringExecutionInnerPodAffinityTerm


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**label_selector** | [**WarmPoolSpecTemplateSpecAffinityPodAffinityPreferredDuringSchedulingIgnoredDuringExecutionInnerPodAffinityTermLabelSelector**](WarmPoolSpecTemplateSpecAffinityPodAffinityPreferredDuringSchedulingIgnoredDuringExecutionInnerPodAffinityTermLabelSelector.md) |  | [optional] 
**match_label_keys** | **List[str]** |  | [optional] 
**mismatch_label_keys** | **List[str]** |  | [optional] 
**namespace_selector** | [**WarmPoolSpecTemplateSpecAffinityPodAffinityPreferredDuringSchedulingIgnoredDuringExecutionInnerPodAffinityTermLabelSelector**](WarmPoolSpecTemplateSpecAffinityPodAffinityPreferredDuringSchedulingIgnoredDuringExecutionInnerPodAffinityTermLabelSelector.md) |  | [optional] 
**namespaces** | **List[str]** |  | [optional] 
**topology_key** | **str** |  | 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_affinity_pod_affinity_preferred_during_scheduling_ignored_during_execution_inner_pod_affinity_term import WarmPoolSpecTemplateSpecAffinityPodAffinityPreferredDuringSchedulingIgnoredDuringExecutionInnerPodAffinityTerm

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecAffinityPodAffinityPreferredDuringSchedulingIgnoredDuringExecutionInnerPodAffinityTerm from a JSON string
warm_pool_spec_template_spec_affinity_pod_affinity_preferred_during_scheduling_ignored_during_execution_inner_pod_affinity_term_instance = WarmPoolSpecTemplateSpecAffinityPodAffinityPreferredDuringSchedulingIgnoredDuringExecutionInnerPodAffinityTerm.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecAffinityPodAffinityPreferredDuringSchedulingIgnoredDuringExecutionInnerPodAffinityTerm.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_affinity_pod_affinity_preferred_during_scheduling_ignored_during_execution_inner_pod_affinity_term_dict = warm_pool_spec_template_spec_affinity_pod_affinity_preferred_during_scheduling_ignored_during_execution_inner_pod_affinity_term_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecAffinityPodAffinityPreferredDuringSchedulingIgnoredDuringExecutionInnerPodAffinityTerm from a dict
warm_pool_spec_template_spec_affinity_pod_affinity_preferred_during_scheduling_ignored_during_execution_inner_pod_affinity_term_from_dict = WarmPoolSpecTemplateSpecAffinityPodAffinityPreferredDuringSchedulingIgnoredDuringExecutionInnerPodAffinityTerm.from_dict(warm_pool_spec_template_spec_affinity_pod_affinity_preferred_during_scheduling_ignored_during_execution_inner_pod_affinity_term_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


