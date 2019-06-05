// Copyright (c) Facebook, Inc. and its affiliates.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package nvdjson

import (
	"fmt"
	"strings"

	"github.com/facebookincubator/nvdtools/cvefeed/jsonschema"
	"github.com/facebookincubator/nvdtools/cvefeed/nvdcommon"
	"github.com/facebookincubator/nvdtools/wfn"
)

type cveItem struct {
	cveItem     *jsonschema.NVDCVEFeedJSON10DefCVEItem
	configNodes []nvdcommon.LogicalTest
}

type node struct {
	node              *jsonschema.NVDCVEFeedJSON10DefNode
	nvdcommonChildren []nvdcommon.LogicalTest
	wfnCPEs           []*wfn.Attributes
}

type cpeMatch struct {
	cpeMatch *jsonschema.NVDCVEFeedJSON10DefCPEMatch
	wfname   *wfn.Attributes
}

func newCveItem(json *jsonschema.NVDCVEFeedJSON10DefCVEItem) nvdcommon.CVEItem {
	item := &cveItem{cveItem: json}
	for _, n := range item.cveItem.Configurations.Nodes {
		item.configNodes = append(item.configNodes, nvdcommon.LogicalTest(newNode(n)))
	}
	return item
}

func newNode(json *jsonschema.NVDCVEFeedJSON10DefNode) nvdcommon.LogicalTest {
	n := &node{node: json}

	if len(n.node.Children) != 0 {
		children := make([]nvdcommon.LogicalTest, len(n.node.Children))
		for i, child := range n.node.Children {
			children[i] = nvdcommon.LogicalTest(&node{node: child})
		}
		n.nvdcommonChildren = children
	}

	if len(n.node.CPEMatch) != 0 {
		cpes := make([]*wfn.Attributes, len(n.node.CPEMatch))
		for i, node := range n.node.CPEMatch {
			cpe, err := node2CPE(&cpeMatch{cpeMatch: node})
			if err == nil {
				cpes[i] = cpe
			}
		}
		n.wfnCPEs = cpes
	}

	return n
}

// CVEID returns the identifier of the vulnerability (e.g. CVE).
func (i *cveItem) CVEID() string {
	if i == nil {
		return ""
	}
	return i.cveItem.CVE.CVEDataMeta.ID
}

// Config returns a set of tests that identify vulnerable platform.
func (i *cveItem) Config() []nvdcommon.LogicalTest {
	if i == nil {
		return nil
	}
	return i.configNodes
}

// ProblemTypes returns weakness types associated with vulnerability (e.g. CWE)
func (i *cveItem) ProblemTypes() []string {
	var cwes []string
	if i.cveItem.CVE == nil || i.cveItem.CVE.CVEDataMeta == nil || i.cveItem.CVE.CVEDataMeta.ID == "" {
		return nil
	}

	if i.cveItem.CVE.Problemtype != nil {
		for _, pt := range i.cveItem.CVE.Problemtype.ProblemtypeData {
			if pt != nil {
				cwe := getLangStr(pt.Description)
				cwes = append(cwes, cwe)
			}
		}
	}
	return cwes
}

// Description returns English written description of vulnerability
func (i *cveItem) Description() string {
	for _, dsc := range i.cveItem.CVE.Description.DescriptionData {
		return dsc.Value
	}
	return ""
}

// CVSS20base returns CVSS 2.0 base score of vulnerability
func (i *cveItem) CVSS20base() float64 {
	if i.cveItem.Impact != nil && i.cveItem.Impact.BaseMetricV2 != nil && i.cveItem.Impact.BaseMetricV2.CVSSV2 != nil {
		return i.cveItem.Impact.BaseMetricV2.CVSSV2.BaseScore
	}
	return 0.0
}

// CVSS30base returns CVSS 3.0 base score of vulnerability
func (i *cveItem) CVSS30base() float64 {
	// find CVSSv3 base score
	if i.cveItem.Impact != nil && i.cveItem.Impact.BaseMetricV3 != nil && i.cveItem.Impact.BaseMetricV3.CVSSV3 != nil {
		return i.cveItem.Impact.BaseMetricV3.CVSSV3.BaseScore
	}
	return 0.0
}

// LogicalOperator implements part of cvefeed.LogicalTest interface
func (n *node) LogicalOperator() string {
	if n == nil {
		return ""
	}
	return n.node.Operator
}

// NegateIfNeeded implements part of cvefeed.LogicalTest interface
func (n *node) NegateIfNeeded(b bool) bool {
	if n == nil || !n.node.Negate {
		return b
	}
	return !b
}

// InnerTests implements part of cvefeed.LogicalTest interface
func (n *node) InnerTests() []nvdcommon.LogicalTest {
	if n == nil {
		return nil
	}
	return n.nvdcommonChildren
}

// CPEs implements part of cvefeed.LogicalTest interface
func (n *node) CPEs() []*wfn.Attributes {
	if n == nil {
		return nil
	}
	return n.wfnCPEs
}

// MatchPlatform implements part of cvefeed.LogicalTest interface
func (n *node) MatchPlatform(platform *wfn.Attributes, requireVersion bool) bool {
	if n == nil {
		return false
	}
	for _, cpeNode := range n.node.CPEMatch {
		cpe, err := node2CPE(&cpeMatch{cpeMatch: cpeNode})
		if err != nil {
			continue
		}
		// Not sure if this is needed, in the feed whenever there is a version constraints, version attributes is already ANY,
		// but better safe, than sorry.
		if cpeNode.VersionStartIncluding != "" || cpeNode.VersionStartExcluding != "" ||
			cpeNode.VersionEndIncluding != "" || cpeNode.VersionEndExcluding != "" {
			cpe.Version = wfn.Any
		} else if requireVersion && cpe.Version == wfn.Any {
			continue
		}
		if wfn.Match(cpe, platform) {
			if platform.Version == wfn.Any || platform.Version == wfn.NA {
				// logical value of N/A only matches logical value of ANY, so technically, this should
				// return platform.Version == wfn.Any || cpe.Version == wfn.Any
				// but these checks have already been performed by wfn.Match() above
				return true
			}
			if cpeNode.VersionStartIncluding == "" && cpeNode.VersionStartExcluding == "" &&
				cpeNode.VersionEndIncluding == "" && cpeNode.VersionEndExcluding == "" {
				return true
			}
			if cpe.Version == wfn.NA {
				return false
			}
			ver := wfn.StripSlashes(platform.Version)
			if cpeNode.VersionStartIncluding != "" && smartVerCmp(ver, cpeNode.VersionStartIncluding) < 0 {
				continue
			}
			if cpeNode.VersionStartExcluding != "" && smartVerCmp(ver, cpeNode.VersionStartExcluding) <= 0 {
				continue
			}
			if cpeNode.VersionEndIncluding != "" && smartVerCmp(ver, cpeNode.VersionEndIncluding) > 0 {
				continue
			}
			if cpeNode.VersionEndExcluding != "" && smartVerCmp(ver, cpeNode.VersionEndExcluding) >= 0 {
				continue
			}
			return true
		}
	}
	return false
}

func node2CPE(node *cpeMatch) (*wfn.Attributes, error) {
	var err error
	if node == nil {
		return nil, fmt.Errorf("cannot collect CPEs from nil node")
	}
	if node.wfname != nil {
		return node.wfname, nil
	}
	uri := node.cpeMatch.Cpe23Uri
	if uri == "" {
		uri = node.cpeMatch.Cpe22Uri
	}
	node.wfname, err = wfn.Parse(uri)
	return node.wfname, err
}

// smartVerCmp compares stringified versions of software.
// It tries to do the right thing for any type of versioning,
// assuming v1 and v2 have the same version convension.
// It will return meaningful result for "95SE" vs "98SP1" or for "16.3.2" vs. "3.7.0",
// but not for "2000" vs "11.7".
// Returns -1 if v1 < v2, 1 if v1 > v2 and 0 if v1 == v2.
func smartVerCmp(v1, v2 string) int {
	for s1, s2 := v1, v2; len(s1) > 0 && len(s2) > 0; {
		num1, cmpTo1, skip1 := parseVerParts(s1)
		num2, cmpTo2, skip2 := parseVerParts(s2)
		if num1 > num2 {
			return 1
		}
		if num2 > num1 {
			return -1
		}
		if cmp := strings.Compare(s1[:cmpTo1], s2[:cmpTo2]); cmp != 0 {
			return cmp
		}
		s1 = s1[skip1:]
		s2 = s2[skip2:]
	}
	// everything is equal so far, the longest wins
	if len(v1) > len(v2) {
		return 1
	}
	if len(v2) > len(v1) {
		return -1
	}
	return 0
}

// parseVerParts returns the length of consecutive run of digits in the beginning of the string,
// the last non-separator chararcted (which should be compared), and index at which the version part (major, minor etc.) ends,
// i.e. the position of the dot or end of the line.
// E.g. parseVerParts("11b.4.16-New_Year_Edition") will return (2, 3, 4)
func parseVerParts(v string) (int, int, int) {
	var num int
	for num = 0; num < len(v); num++ {
		if v[num] < '0' || v[num] > '9' {
			break
		}
	}
	if num == len(v) {
		return num, num, num
	}
	// Any punctuation separates the parts.
	skip := strings.IndexFunc(v, func(b rune) bool {
		// !"#$%&'()*+,-./ are dec 33 to 47, :;<=>?@ are dec 58 to 64, [\]^_` are dec 91 to 96 and {|}~ are dec 123 to 126.
		// So, punctuation is in dec 33-126 range except 48-57, 65-90 and 97-122 gaps.
		// This inverse logic allows for early short-circuting for most of the chars and shaves ~20ns in benchmarks.
		return b >= '!' && b <= '~' &&
			!(b > '/' && b < ':' ||
				b > '@' && b < '[' ||
				b > '`' && b < '{')
	})
	if skip == -1 {
		return num, len(v), len(v)
	}
	return num, skip, skip + 1
}

func getLangStr(lss []*jsonschema.CVEJSON40LangString) string {
	var s string
	for _, ls := range lss {
		if ls == nil {
			continue
		}
		s = ls.Value
		if ls.Lang == "en" {
			break
		}
	}
	return s
}
