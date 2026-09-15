// Package catia is the leaf CATIA kind table, format detection and accessible-text extractor.
//
// It imports no other internal package. The scanner uses KindOf to set is_catia from the last
// dotted extension. Split calls Extract on the placed destination copy. Specification:
// docs/openspec/stage-4-catia/catia.md.
package catia
