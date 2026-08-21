package validation

import (
	"fmt"
	"sort"
	"sync"

	"scrinium/internal/knowledge"
)

type registration struct {
	validator  Validator
	descriptor Descriptor
}

type Registry struct {
	mu         sync.RWMutex
	validators map[string]registration
}

func NewRegistry() *Registry {
	return &Registry{validators: make(map[string]registration)}
}

func (r *Registry) Register(validator Validator) error {
	if validator == nil {
		return validationError("nil_validator", "validator must not be nil")
	}
	descriptor := normalizeDescriptor(validator.Descriptor())
	if err := descriptor.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.validators[descriptor.ID]; exists {
		return validationError("duplicate_validator", fmt.Sprintf("validator %s is already registered", descriptor.ID))
	}
	r.validators[descriptor.ID] = registration{validator: validator, descriptor: descriptor}
	return nil
}

func (r *Registry) Resolve(id string) (Validator, Descriptor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, exists := r.validators[id]
	if !exists {
		return nil, Descriptor{}, false
	}
	return entry.validator, normalizeDescriptor(entry.descriptor), true
}

func (r *Registry) Descriptor(id string) (Descriptor, bool) {
	_, descriptor, exists := r.Resolve(id)
	return descriptor, exists
}

func (r *Registry) Descriptors() []Descriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	descriptors := make([]Descriptor, 0, len(r.validators))
	for _, entry := range r.validators {
		descriptors = append(descriptors, normalizeDescriptor(entry.descriptor))
	}
	sort.Slice(descriptors, func(i, j int) bool { return descriptors[i].ID < descriptors[j].ID })
	return descriptors
}

func (r *Registry) ResolveBinding(binding knowledge.ValidationBinding) (Validator, Descriptor, error) {
	validator, descriptor, exists := r.Resolve(binding.ValidatorID)
	if !exists {
		return nil, Descriptor{}, validationError("validator_unavailable", fmt.Sprintf("validator %s is not registered", binding.ValidatorID))
	}
	if !descriptor.SupportsBindingVersion(binding.BindingVersion) {
		return nil, descriptor, validationError("unsupported_binding_version", fmt.Sprintf("validator %s does not support binding version %s", descriptor.ID, binding.BindingVersion))
	}
	if !AssuranceAllowed(binding.RequiredAssurance, descriptor.MaximumAssurance) {
		return nil, descriptor, validationError("binding_assurance_above_ceiling", fmt.Sprintf("binding %s requires assurance above validator %s ceiling", binding.ID, descriptor.ID))
	}
	if err := ValidateBinding(validator, binding); err != nil {
		return nil, descriptor, err
	}
	return validator, descriptor, nil
}
