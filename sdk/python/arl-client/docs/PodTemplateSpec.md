# PodTemplateSpec


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**metadata** | [**ObjectMeta**](ObjectMeta.md) |  | [optional] 
**spec** | **object** | Pod specification - simplified for SDK | [optional] 

## Example

```python
from arl_client.models.pod_template_spec import PodTemplateSpec

# TODO update the JSON string below
json = "{}"
# create an instance of PodTemplateSpec from a JSON string
pod_template_spec_instance = PodTemplateSpec.from_json(json)
# print the JSON string representation of the object
print(PodTemplateSpec.to_json())

# convert the object into a dict
pod_template_spec_dict = pod_template_spec_instance.to_dict()
# create an instance of PodTemplateSpec from a dict
pod_template_spec_from_dict = PodTemplateSpec.from_dict(pod_template_spec_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


