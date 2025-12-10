package mso

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"

	"github.com/ciscoecosystem/mso-go-client/client"
	"github.com/ciscoecosystem/mso-go-client/container"
	"github.com/ciscoecosystem/mso-go-client/models"
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/helper/validation"
)

func resourceMSOTemplateAnpEpgContract() *schema.Resource {
	return &schema.Resource{
		Create: resourceMSOTemplateAnpEpgContractCreate,
		Read:   resourceMSOTemplateAnpEpgContractRead,
		Update: resourceMSOTemplateAnpEpgContractUpdate,
		Delete: resourceMSOTemplateAnpEpgContractDelete,

		Importer: &schema.ResourceImporter{
			State: resourceMSOTemplateAnpEpgContractImport,
		},

		SchemaVersion: version,

		Schema: (map[string]*schema.Schema{
			"schema_id": &schema.Schema{
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validation.StringLenBetween(1, 1000),
			},
			"template_name": &schema.Schema{
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validation.StringLenBetween(1, 1000),
			},
			"anp_name": &schema.Schema{
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validation.StringLenBetween(1, 1000),
			},
			"epg_name": &schema.Schema{
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validation.StringLenBetween(1, 1000),
			},
			"contract_name": &schema.Schema{
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validation.StringLenBetween(1, 1000),
			},
			"contract_schema_id": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
			},
			"contract_template_name": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
			},
			"relationship_type": &schema.Schema{
				Type:         schema.TypeString,
				Required:     true,
				ValidateFunc: validation.StringLenBetween(1, 1000),
			},
		}),
	}

}

func resourceMSOTemplateAnpEpgContractImport(d *schema.ResourceData, m interface{}) ([]*schema.ResourceData, error) {
	log.Printf("[DEBUG] %s: Beginning Import", d.Id())

	msoClient := m.(*client.Client)
	get_attribute := strings.Split(d.Id(), "/")
	schemaId := get_attribute[0]
	stateTemplate := get_attribute[2]
	stateANP := get_attribute[4]
	stateEPG := get_attribute[6]
	stateContract := get_attribute[8]
	stateRelationshipType := get_attribute[10]

	// Use the new specific EPG endpoint
	cont, err := msoClient.GetViaURL(fmt.Sprintf("api/v1/schemas/%s/templates/%s/anps/%s/epgs/%s", schemaId, stateTemplate, stateANP, stateEPG))
	if err != nil {
		return nil, err
	}

	// Set the basic resource attributes
	d.Set("schema_id", schemaId)
	d.Set("template_name", stateTemplate)
	d.Set("anp_name", stateANP)
	d.Set("epg_name", stateEPG)

	// Check if the response contains the EPG data wrapped in "epg" object
	if !cont.Exists("epg") {
		return nil, fmt.Errorf("Unable to find EPG data in response for EPG %s in ANP %s, Template %s of Schema Id %s", stateEPG, stateANP, stateTemplate, schemaId)
	}

	// Get the EPG container directly from the "epg" wrapper
	epgCont := cont.S("epg")

	found := false
	// Look through the contract relationships in this EPG
	crefCount, err := epgCont.ArrayCount("contractRelationships")
	if err != nil {
		return nil, fmt.Errorf("Unable to get the contract relationships list")
	}

	for l := 0; l < crefCount; l++ {
		crefCont, err := epgCont.ArrayElement(l, "contractRelationships")
		if err != nil {
			return nil, err
		}
		contractRef := models.StripQuotes(crefCont.S("contractRef").String())
		apiRelationshipType := models.StripQuotes(crefCont.S("relationshipType").String())
		re := regexp.MustCompile("/schemas/(.*)/templates/(.*)/contracts/(.*)")
		match := re.FindStringSubmatch(contractRef)
		if len(match) != 4 {
			continue // Skip if regex doesn't match expected format
		}
		apiContract := match[3]
		if apiContract == stateContract && apiRelationshipType == stateRelationshipType {
			d.SetId(apiContract)
			d.Set("contract_name", match[3])
			d.Set("contract_schema_id", match[1])
			d.Set("contract_template_name", match[2])
			d.Set("relationship_type", apiRelationshipType)
			found = true
			break
		}
	}

	if !found {
		d.SetId("")
		return nil, fmt.Errorf("Unable to find the Contract %s", stateContract)
	}

	log.Printf("[DEBUG] %s: Import finished successfully", d.Id())
	return []*schema.ResourceData{d}, nil
}

func resourceMSOTemplateAnpEpgContractCreate(d *schema.ResourceData, m interface{}) error {
	log.Printf("[DEBUG] Template BD: Beginning Creation")
	msoClient := m.(*client.Client)

	schemaID := d.Get("schema_id").(string)
	templateName := d.Get("template_name").(string)
	anpName := d.Get("anp_name").(string)
	epgName := d.Get("epg_name").(string)
	contractName := d.Get("contract_name").(string)

	var relationship_type, contract_schemaid, contract_templatename string
	if tempVar, ok := d.GetOk("relationship_type"); ok {
		relationship_type = tempVar.(string)
	}

	if tempVar, ok := d.GetOk("contract_schema_id"); ok {
		contract_schemaid = tempVar.(string)
	} else {
		contract_schemaid = schemaID
	}
	if tempVar, ok := d.GetOk("contract_template_name"); ok {
		contract_templatename = tempVar.(string)
	} else {
		contract_templatename = templateName
	}

	contractRefMap := make(map[string]interface{})
	contractRefMap["schemaId"] = contract_schemaid
	contractRefMap["templateName"] = contract_templatename
	contractRefMap["contractName"] = contractName

	path := fmt.Sprintf("/templates/%s/anps/%s/epgs/%s/contractRelationships/-", templateName, anpName, epgName)
	bdStruct := models.NewTemplateAnpEpgContract("add", path, contractRefMap, relationship_type)

	_, err := msoClient.PatchbyID(fmt.Sprintf("api/v1/schemas/%s", schemaID), bdStruct)
	if err != nil {
		return err
	}
	return resourceMSOTemplateAnpEpgContractRead(d, m)
}

func resourceMSOTemplateAnpEpgContractRead(d *schema.ResourceData, m interface{}) error {
	log.Printf("[DEBUG] %s: Beginning Read", d.Id())

	msoClient := m.(*client.Client)

	schemaId := d.Get("schema_id").(string)
	stateTemplate := d.Get("template_name").(string)
	stateANP := d.Get("anp_name").(string)
	stateEPG := d.Get("epg_name").(string)
	stateContract := d.Get("contract_name").(string)
	stateRelationshipType := d.Get("relationship_type").(string)

	// Use the new specific EPG endpoint
	cont, err := msoClient.GetViaURL(fmt.Sprintf("api/v1/schemas/%s/templates/%s/anps/%s/epgs/%s", schemaId, stateTemplate, stateANP, stateEPG))
	if err != nil {
		return errorForObjectNotFound(err, d.Id(), cont, d)
	}

	// Check if the response contains the EPG data wrapped in "epg" object
	if !cont.Exists("epg") {
		return fmt.Errorf("Unable to find EPG data in response for EPG %s in ANP %s, Template %s of Schema Id %s", stateEPG, stateANP, stateTemplate, schemaId)
	}

	// Get the EPG container directly from the "epg" wrapper
	epgCont := cont.S("epg")

	found := false
	// Look through the contract relationships in this EPG
	crefCount, err := epgCont.ArrayCount("contractRelationships")
	if err != nil {
		return fmt.Errorf("Unable to get the contract relationships list")
	}

	for l := 0; l < crefCount; l++ {
		crefCont, err := epgCont.ArrayElement(l, "contractRelationships")
		if err != nil {
			return err
		}
		contractRef := models.StripQuotes(crefCont.S("contractRef").String())
		apiRelationshipType := models.StripQuotes(crefCont.S("relationshipType").String())
		re := regexp.MustCompile("/schemas/(.*)/templates/(.*)/contracts/(.*)")
		match := re.FindStringSubmatch(contractRef)
		if len(match) != 4 {
			continue // Skip if regex doesn't match expected format
		}
		apiContract := match[3]
		if apiContract == stateContract && apiRelationshipType == stateRelationshipType {
			d.SetId(apiContract)
			d.Set("contract_name", match[3])
			d.Set("contract_schema_id", match[1])
			d.Set("contract_template_name", match[2])
			d.Set("relationship_type", apiRelationshipType)
			found = true
			break
		}
	}

	if !found {
		d.SetId("")
	}

	log.Printf("[DEBUG] %s: Read finished successfully", d.Id())
	return nil
}

func resourceMSOTemplateAnpEpgContractUpdate(d *schema.ResourceData, m interface{}) error {
	log.Printf("[DEBUG] Template BD: Beginning Update")
	msoClient := m.(*client.Client)

	schemaID := d.Get("schema_id").(string)
	templateName := d.Get("template_name").(string)
	anpName := d.Get("anp_name").(string)
	epgName := d.Get("epg_name").(string)
	contractName := d.Get("contract_name").(string)

	var relationship_type, contract_schemaid, contract_templatename string
	if tempVar, ok := d.GetOk("relationship_type"); ok {
		relationship_type = tempVar.(string)
	}

	if tempVar, ok := d.GetOk("contract_schema_id"); ok {
		contract_schemaid = tempVar.(string)
	} else {
		contract_schemaid = schemaID
	}
	if tempVar, ok := d.GetOk("contract_template_name"); ok {
		contract_templatename = tempVar.(string)
	} else {
		contract_templatename = templateName
	}

	contractRefMap := make(map[string]interface{})
	contractRefMap["schemaId"] = contract_schemaid
	contractRefMap["templateName"] = contract_templatename
	contractRefMap["contractName"] = contractName

	id := d.Id()
	// Use the new specific EPG endpoint
	cont, err := msoClient.GetViaURL(fmt.Sprintf("api/v1/schemas/%s/templates/%s/anps/%s/epgs/%s", schemaID, templateName, anpName, epgName))
	if err != nil {
		return err
	}

	// Check if the response contains the EPG data wrapped in "epg" object
	if !cont.Exists("epg") {
		return fmt.Errorf("Unable to find EPG data in response for EPG %s in ANP %s, Template %s of Schema Id %s", epgName, anpName, templateName, schemaID)
	}

	// Get the EPG container directly from the "epg" wrapper
	epgCont := cont.S("epg")

	index, err := fetchindex(epgCont, id, relationship_type)
	if err != nil {
		return err
	}
	if index == -1 {
		return fmt.Errorf("The given contract id is not found")
	}
	indexs := strconv.Itoa(index)

	path := fmt.Sprintf("/templates/%s/anps/%s/epgs/%s/contractRelationships/%s", templateName, anpName, epgName, indexs)
	crefStruct := models.NewTemplateAnpEpgContract("replace", path, contractRefMap, relationship_type)

	_, errs := msoClient.PatchbyID(fmt.Sprintf("api/v1/schemas/%s", schemaID), crefStruct)
	if errs != nil {
		return errs
	}
	return resourceMSOTemplateAnpEpgContractRead(d, m)
}

func resourceMSOTemplateAnpEpgContractDelete(d *schema.ResourceData, m interface{}) error {
	log.Printf("[DEBUG] Template ANP EPG Contract: Beginning Delete")
	msoClient := m.(*client.Client)

	schemaID := d.Get("schema_id").(string)
	templateName := d.Get("template_name").(string)
	anpName := d.Get("anp_name").(string)
	epgName := d.Get("epg_name").(string)
	contractName := d.Get("contract_name").(string)

	var relationship_type, contract_schemaid, contract_templatename string
	if tempVar, ok := d.GetOk("relationship_type"); ok {
		relationship_type = tempVar.(string)
	}

	if tempVar, ok := d.GetOk("contract_schema_id"); ok {
		contract_schemaid = tempVar.(string)
	} else {
		contract_schemaid = schemaID
	}
	if tempVar, ok := d.GetOk("contract_template_name"); ok {
		contract_templatename = tempVar.(string)
	} else {
		contract_templatename = templateName
	}

	contractRefMap := make(map[string]interface{})
	contractRefMap["schemaId"] = contract_schemaid
	contractRefMap["templateName"] = contract_templatename
	contractRefMap["contractName"] = contractName

	id := d.Id()
	// Use the new specific EPG endpoint
	cont, err := msoClient.GetViaURL(fmt.Sprintf("api/v1/schemas/%s/templates/%s/anps/%s/epgs/%s", schemaID, templateName, anpName, epgName))
	if err != nil {
		return err
	}

	// Check if the response contains the EPG data wrapped in "epg" object
	if !cont.Exists("epg") {
		return fmt.Errorf("Unable to find EPG data in response for EPG %s in ANP %s, Template %s of Schema Id %s", epgName, anpName, templateName, schemaID)
	}

	// Get the EPG container directly from the "epg" wrapper
	epgCont := cont.S("epg")

	index, err := fetchindex(epgCont, id, relationship_type)
	if err != nil {
		return err
	}
	if index == -1 {
		d.SetId("")
		return nil
	}
	indexs := strconv.Itoa(index)

	path := fmt.Sprintf("/templates/%s/anps/%s/epgs/%s/contractRelationships/%s", templateName, anpName, epgName, indexs)
	crefStruct := models.NewTemplateAnpEpgContract("remove", path, contractRefMap, relationship_type)

	response, errs := msoClient.PatchbyID(fmt.Sprintf("api/v1/schemas/%s", schemaID), crefStruct)

	// Ignoring Error with code 141: Resource Not Found when deleting
	if errs != nil && !(response.Exists("code") && response.S("code").String() == "141") {
		return errs
	}
	d.SetId("")
	return resourceMSOTemplateAnpEpgContractRead(d, m)
}

func fetchindex(epgCont *container.Container, contractName, relationship_type string) (int, error) {
	index := -1

	contractCount, err := epgCont.ArrayCount("contractRelationships")
	if err != nil {
		return index, fmt.Errorf("No contractRelationships found")
	}

	for s := 0; s < contractCount; s++ {
		contractCont, err := epgCont.ArrayElement(s, "contractRelationships")
		if err != nil {
			return index, err
		}
		contractRef := models.StripQuotes(contractCont.S("contractRef").String())
		apiRelationshipType := models.StripQuotes(contractCont.S("relationshipType").String())
		re := regexp.MustCompile("/schemas/(.*)/templates/(.*)/contracts/(.*)")
		match := re.FindStringSubmatch(contractRef)
		if len(match) != 4 {
			continue // Skip if regex doesn't match expected format
		}
		apiContract := match[3]
		if apiContract == contractName && apiRelationshipType == relationship_type {
			index = s
			break
		}
	}
	return index, nil
}
