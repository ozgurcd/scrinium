package gograph

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/token"
	"path"
	"strings"
	"unicode"

	"scrinium/internal/knowledge"
	"scrinium/internal/validation"
)

func parseBinding(candidate knowledge.ValidationBinding) (binding, error) {
	if candidate.ValidatorID != ValidatorID {
		return binding{}, bindingError("invalid_binding_schema", "binding validator_id must be gograph")
	}
	if candidate.BindingVersion != BindingSchemaVersion {
		return binding{}, bindingError("unsupported_binding_version", "unsupported Gograph binding version")
	}
	for key := range candidate.Parameters {
		switch key {
		case "predicate", "object", "required_precision", "target":
		default:
			return binding{}, bindingError("invalid_binding_schema", "Gograph binding contains unsupported parameter "+key)
		}
	}
	target := candidate.Parameters["target"]
	if target != "" && !targetNamePattern.MatchString(target) {
		return binding{}, bindingError("invalid_binding_schema", "Gograph target must be an abstract allowlisted name, never a filesystem path")
	}

	predicate := candidate.Parameters["predicate"]
	precision := candidate.Parameters["required_precision"]
	if precision != precisionAST && precision != precisionPrecise {
		return binding{}, bindingError("invalid_binding_schema", "Gograph required_precision must be ast or precise")
	}

	subjectKind, objectKind, needsObject, err := predicateShape(predicate)
	if err != nil {
		return binding{}, err
	}
	if (predicate == predicateCallEdge || predicate == predicateTypeImplements) && precision != precisionPrecise {
		return binding{}, bindingError("invalid_binding_schema", predicate+" requires precise analysis")
	}
	if err := validateIdentity(subjectKind, candidate.Reference); err != nil {
		return binding{}, err
	}

	document := bindingDocument{
		SchemaVersion: BindingSchemaVersion,
		Predicate:     predicate,
		Subject: referenceDocument{
			Language: "go",
			Kind:     subjectKind,
			ID:       candidate.Reference,
		},
		RequiredPrecision: precision,
	}
	objectID, hasObject := candidate.Parameters["object"]
	if needsObject {
		if !hasObject || objectID == "" {
			return binding{}, bindingError("invalid_binding_schema", predicate+" requires an object identity")
		}
		if err := validateIdentity(objectKind, objectID); err != nil {
			return binding{}, err
		}
		document.Object = &referenceDocument{Language: "go", Kind: objectKind, ID: objectID}
	} else if hasObject {
		return binding{}, bindingError("invalid_binding_schema", predicate+" does not accept an object identity")
	}

	encoded, err := json.Marshal(document)
	if err != nil {
		return binding{}, bindingError("invalid_binding_schema", "Gograph binding could not be encoded")
	}
	digest := sha256.Sum256(append(encoded, '\n'))
	return binding{document: document, fingerprint: hex.EncodeToString(digest[:]), json: string(encoded), target: target}, nil
}

func predicateShape(predicate string) (subjectKind, objectKind string, needsObject bool, err error) {
	switch predicate {
	case predicateSymbolExists:
		return referenceSymbol, "", false, nil
	case predicatePackageImports:
		return referencePackage, referencePackage, true, nil
	case predicateCallEdge, predicateTypeImplements:
		return referenceSymbol, referenceSymbol, true, nil
	default:
		return "", "", false, bindingError("unsupported_predicate", "unsupported Gograph predicate "+predicate)
	}
}

func validateIdentity(kind, id string) error {
	switch kind {
	case referencePackage:
		if !validImportPath(id) {
			return bindingError("invalid_package_identity", "Gograph package identity must be an exact Go import path")
		}
	case referenceSymbol:
		if !validSymbolID(id) {
			return bindingError("invalid_symbol_identity", "Gograph symbol identity must be an exact module-qualified SymbolNode.ID")
		}
	default:
		return bindingError("invalid_binding_schema", "Gograph reference kind is invalid")
	}
	return nil
}

func validSymbolID(id string) bool {
	packagePath, declaration, found := strings.Cut(id, "::")
	if !found || strings.Contains(declaration, "::") || !validImportPath(packagePath) {
		return false
	}
	if token.IsIdentifier(declaration) {
		return true
	}
	separator := strings.Index(declaration, ").")
	if !strings.HasPrefix(declaration, "(") || separator < 2 {
		return false
	}
	receiver := strings.TrimPrefix(declaration[1:separator], "*")
	method := declaration[separator+2:]
	return token.IsIdentifier(receiver) && token.IsIdentifier(method)
}

func validImportPath(value string) bool {
	if value == "" || strings.HasPrefix(value, "/") || strings.HasPrefix(value, "_") || path.Clean(value) != value {
		return false
	}
	for _, component := range strings.Split(value, "/") {
		if component == "" || component == "." || component == ".." {
			return false
		}
	}
	for _, character := range value {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || strings.ContainsRune(".-_~", character) || character == '/' {
			continue
		}
		return false
	}
	return true
}

func bindingError(code, message string) error {
	return &validation.Error{Code: code, Message: message}
}

func sameBinding(left, right bindingDocument) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
}

func bindingSummary(document bindingDocument) string {
	if document.Object == nil {
		return fmt.Sprintf("%s %s", document.Predicate, document.Subject.ID)
	}
	return fmt.Sprintf("%s %s -> %s", document.Predicate, document.Subject.ID, document.Object.ID)
}
