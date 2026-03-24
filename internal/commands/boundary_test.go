package commands

import (
	"context"
	"strings"
	"testing"

	"github.com/FlavioCFOliveira/Groadmap/internal/models"
	"github.com/FlavioCFOliveira/Groadmap/internal/utils"
)

// ==================== helpers ====================

// repeatByte returns a string built from n repetitions of the single byte b.
func repeatByte(b byte, n int) string {
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = b
	}
	return string(buf)
}

// baseValidTask returns a minimal valid Task ready for Validate() calls.
func baseValidTask() *models.Task {
	return &models.Task{
		Title:                  "Implement OAuth2 authentication service",
		Status:                 models.StatusBacklog,
		Type:                   models.TypeTask,
		FunctionalRequirements: "Allow users to authenticate via company SSO provider",
		TechnicalRequirements:  "Integrate Auth0 SDK; update session middleware and token refresh logic",
		AcceptanceCriteria:     "Users can log in via SSO; tokens refreshed automatically without re-login",
		Priority:               5,
		Severity:               3,
		CreatedAt:              utils.NowISO8601(),
	}
}

// ==================== models.Task.Validate — priority boundary ====================

func TestTaskValidate_Priority_MinBoundary(t *testing.T) {
	task := baseValidTask()
	task.Priority = 0
	if err := task.Validate(); err != nil {
		t.Errorf("priority 0 (min boundary) should be valid, got: %v", err)
	}
}

func TestTaskValidate_Priority_MaxBoundary(t *testing.T) {
	task := baseValidTask()
	task.Priority = 9
	if err := task.Validate(); err != nil {
		t.Errorf("priority 9 (max boundary) should be valid, got: %v", err)
	}
}

func TestTaskValidate_Priority_BelowMin(t *testing.T) {
	task := baseValidTask()
	task.Priority = -1
	err := task.Validate()
	if err == nil {
		t.Error("priority -1 (below min) should be invalid, got nil")
	}
	if !strings.Contains(err.Error(), "priority must be between 0 and 9") {
		t.Errorf("unexpected error message for priority -1: %v", err)
	}
}

func TestTaskValidate_Priority_AboveMax(t *testing.T) {
	task := baseValidTask()
	task.Priority = 10
	err := task.Validate()
	if err == nil {
		t.Error("priority 10 (above max) should be invalid, got nil")
	}
	if !strings.Contains(err.Error(), "priority must be between 0 and 9") {
		t.Errorf("unexpected error message for priority 10: %v", err)
	}
}

// ==================== models.Task.Validate — severity boundary ====================

func TestTaskValidate_Severity_MinBoundary(t *testing.T) {
	task := baseValidTask()
	task.Severity = 0
	if err := task.Validate(); err != nil {
		t.Errorf("severity 0 (min boundary) should be valid, got: %v", err)
	}
}

func TestTaskValidate_Severity_MaxBoundary(t *testing.T) {
	task := baseValidTask()
	task.Severity = 9
	if err := task.Validate(); err != nil {
		t.Errorf("severity 9 (max boundary) should be valid, got: %v", err)
	}
}

func TestTaskValidate_Severity_BelowMin(t *testing.T) {
	task := baseValidTask()
	task.Severity = -1
	err := task.Validate()
	if err == nil {
		t.Error("severity -1 (below min) should be invalid, got nil")
	}
	if !strings.Contains(err.Error(), "severity must be between 0 and 9") {
		t.Errorf("unexpected error message for severity -1: %v", err)
	}
}

func TestTaskValidate_Severity_AboveMax(t *testing.T) {
	task := baseValidTask()
	task.Severity = 10
	err := task.Validate()
	if err == nil {
		t.Error("severity 10 (above max) should be invalid, got nil")
	}
	if !strings.Contains(err.Error(), "severity must be between 0 and 9") {
		t.Errorf("unexpected error message for severity 10: %v", err)
	}
}

// ==================== models.Task.Validate — title length boundary ====================

func TestTaskValidate_Title_ExactMaxLength(t *testing.T) {
	task := baseValidTask()
	task.Title = repeatByte('a', models.MaxTaskTitle)
	if err := task.Validate(); err != nil {
		t.Errorf("title at exact max (%d bytes) should be valid, got: %v", models.MaxTaskTitle, err)
	}
}

func TestTaskValidate_Title_OneBeyondMaxLength(t *testing.T) {
	task := baseValidTask()
	task.Title = repeatByte('a', models.MaxTaskTitle+1)
	err := task.Validate()
	if err == nil {
		t.Errorf("title at max+1 (%d bytes) should be invalid, got nil", models.MaxTaskTitle+1)
	}
	if !utils.IsFieldTooLarge(err) {
		t.Errorf("expected ErrFieldTooLarge for over-length title, got: %v", err)
	}
}

func TestTaskValidate_Title_Empty(t *testing.T) {
	task := baseValidTask()
	task.Title = ""
	if err := task.Validate(); err == nil {
		t.Error("empty title should be invalid, got nil")
	}
}

func TestTaskValidate_Title_SingleChar(t *testing.T) {
	task := baseValidTask()
	task.Title = "X"
	if err := task.Validate(); err != nil {
		t.Errorf("single-char title should be valid, got: %v", err)
	}
}

// ==================== models.Task.Validate — functional requirements length boundary ====================

func TestTaskValidate_FunctionalRequirements_ExactMaxLength(t *testing.T) {
	task := baseValidTask()
	task.FunctionalRequirements = repeatByte('f', models.MaxTaskFunctionalRequirements)
	if err := task.Validate(); err != nil {
		t.Errorf("functional_requirements at exact max (%d bytes) should be valid, got: %v",
			models.MaxTaskFunctionalRequirements, err)
	}
}

func TestTaskValidate_FunctionalRequirements_OneBeyondMaxLength(t *testing.T) {
	task := baseValidTask()
	task.FunctionalRequirements = repeatByte('f', models.MaxTaskFunctionalRequirements+1)
	err := task.Validate()
	if err == nil {
		t.Errorf("functional_requirements at max+1 (%d bytes) should be invalid, got nil",
			models.MaxTaskFunctionalRequirements+1)
	}
	if !utils.IsFieldTooLarge(err) {
		t.Errorf("expected ErrFieldTooLarge for over-length functional_requirements, got: %v", err)
	}
}

// ==================== models.Task.Validate — technical requirements length boundary ====================

func TestTaskValidate_TechnicalRequirements_ExactMaxLength(t *testing.T) {
	task := baseValidTask()
	task.TechnicalRequirements = repeatByte('t', models.MaxTaskTechnicalRequirements)
	if err := task.Validate(); err != nil {
		t.Errorf("technical_requirements at exact max (%d bytes) should be valid, got: %v",
			models.MaxTaskTechnicalRequirements, err)
	}
}

func TestTaskValidate_TechnicalRequirements_OneBeyondMaxLength(t *testing.T) {
	task := baseValidTask()
	task.TechnicalRequirements = repeatByte('t', models.MaxTaskTechnicalRequirements+1)
	err := task.Validate()
	if err == nil {
		t.Errorf("technical_requirements at max+1 (%d bytes) should be invalid, got nil",
			models.MaxTaskTechnicalRequirements+1)
	}
	if !utils.IsFieldTooLarge(err) {
		t.Errorf("expected ErrFieldTooLarge for over-length technical_requirements, got: %v", err)
	}
}

// ==================== models.Task.Validate — acceptance criteria length boundary ====================

func TestTaskValidate_AcceptanceCriteria_ExactMaxLength(t *testing.T) {
	task := baseValidTask()
	task.AcceptanceCriteria = repeatByte('c', models.MaxTaskAcceptanceCriteria)
	if err := task.Validate(); err != nil {
		t.Errorf("acceptance_criteria at exact max (%d bytes) should be valid, got: %v",
			models.MaxTaskAcceptanceCriteria, err)
	}
}

func TestTaskValidate_AcceptanceCriteria_OneBeyondMaxLength(t *testing.T) {
	task := baseValidTask()
	task.AcceptanceCriteria = repeatByte('c', models.MaxTaskAcceptanceCriteria+1)
	err := task.Validate()
	if err == nil {
		t.Errorf("acceptance_criteria at max+1 (%d bytes) should be invalid, got nil",
			models.MaxTaskAcceptanceCriteria+1)
	}
	if !utils.IsFieldTooLarge(err) {
		t.Errorf("expected ErrFieldTooLarge for over-length acceptance_criteria, got: %v", err)
	}
}

// ==================== models.Task.Validate — specialists length boundary ====================

func TestTaskValidate_Specialists_ExactMaxLength(t *testing.T) {
	task := baseValidTask()
	s := repeatByte('s', models.MaxTaskSpecialists)
	task.Specialists = &s
	if err := task.Validate(); err != nil {
		t.Errorf("specialists at exact max (%d bytes) should be valid, got: %v",
			models.MaxTaskSpecialists, err)
	}
}

func TestTaskValidate_Specialists_OneBeyondMaxLength(t *testing.T) {
	task := baseValidTask()
	s := repeatByte('s', models.MaxTaskSpecialists+1)
	task.Specialists = &s
	err := task.Validate()
	if err == nil {
		t.Errorf("specialists at max+1 (%d bytes) should be invalid, got nil",
			models.MaxTaskSpecialists+1)
	}
	if !utils.IsFieldTooLarge(err) {
		t.Errorf("expected ErrFieldTooLarge for over-length specialists, got: %v", err)
	}
}

func TestTaskValidate_Specialists_Nil(t *testing.T) {
	task := baseValidTask()
	task.Specialists = nil
	if err := task.Validate(); err != nil {
		t.Errorf("nil specialists (optional field) should be valid, got: %v", err)
	}
}

// ==================== models.TaskUpdate.Validate — priority/severity boundary ====================

func TestTaskUpdateValidate_Priority_MinBoundary(t *testing.T) {
	p := 0
	u := models.TaskUpdate{Priority: &p}
	if err := u.Validate(); err != nil {
		t.Errorf("TaskUpdate priority 0 should be valid, got: %v", err)
	}
}

func TestTaskUpdateValidate_Priority_MaxBoundary(t *testing.T) {
	p := 9
	u := models.TaskUpdate{Priority: &p}
	if err := u.Validate(); err != nil {
		t.Errorf("TaskUpdate priority 9 should be valid, got: %v", err)
	}
}

func TestTaskUpdateValidate_Priority_BelowMin(t *testing.T) {
	p := -1
	u := models.TaskUpdate{Priority: &p}
	err := u.Validate()
	if err == nil {
		t.Error("TaskUpdate priority -1 should be invalid, got nil")
	}
	if !strings.Contains(err.Error(), "priority must be between 0 and 9") {
		t.Errorf("unexpected error for TaskUpdate priority -1: %v", err)
	}
}

func TestTaskUpdateValidate_Priority_AboveMax(t *testing.T) {
	p := 10
	u := models.TaskUpdate{Priority: &p}
	err := u.Validate()
	if err == nil {
		t.Error("TaskUpdate priority 10 should be invalid, got nil")
	}
	if !strings.Contains(err.Error(), "priority must be between 0 and 9") {
		t.Errorf("unexpected error for TaskUpdate priority 10: %v", err)
	}
}

func TestTaskUpdateValidate_Severity_BelowMin(t *testing.T) {
	s := -1
	u := models.TaskUpdate{Severity: &s}
	err := u.Validate()
	if err == nil {
		t.Error("TaskUpdate severity -1 should be invalid, got nil")
	}
	if !strings.Contains(err.Error(), "severity must be between 0 and 9") {
		t.Errorf("unexpected error for TaskUpdate severity -1: %v", err)
	}
}

func TestTaskUpdateValidate_Severity_AboveMax(t *testing.T) {
	s := 10
	u := models.TaskUpdate{Severity: &s}
	err := u.Validate()
	if err == nil {
		t.Error("TaskUpdate severity 10 should be invalid, got nil")
	}
	if !strings.Contains(err.Error(), "severity must be between 0 and 9") {
		t.Errorf("unexpected error for TaskUpdate severity 10: %v", err)
	}
}

// ==================== Unicode input — model-level validation ====================

func TestTaskValidate_Unicode_TitleCJK(t *testing.T) {
	task := baseValidTask()
	// CJK characters — each is 3 UTF-8 bytes; 85 chars = 255 bytes = MaxTaskTitle.
	task.Title = strings.Repeat("中", 85)
	if err := task.Validate(); err != nil {
		t.Errorf("CJK title at exact byte boundary should be valid, got: %v", err)
	}
}

func TestTaskValidate_Unicode_TitleRTL(t *testing.T) {
	task := baseValidTask()
	task.Title = "تنفيذ خدمة المصادقة على مجموعة الإنتاج"
	if err := task.Validate(); err != nil {
		t.Errorf("Arabic RTL title should be valid, got: %v", err)
	}
}

func TestTaskValidate_Unicode_TitleAccented(t *testing.T) {
	task := baseValidTask()
	task.Title = "Implementação do serviço OAuth2 (português)"
	if err := task.Validate(); err != nil {
		t.Errorf("Latin/accented title should be valid, got: %v", err)
	}
}

func TestTaskValidate_Unicode_DescriptionCJK(t *testing.T) {
	task := baseValidTask()
	task.FunctionalRequirements = strings.Repeat("需求", 100)
	if err := task.Validate(); err != nil {
		t.Errorf("CJK functional_requirements should be valid, got: %v", err)
	}
}

func TestTaskValidate_Unicode_NullByteInTitle(t *testing.T) {
	task := baseValidTask()
	// Null byte embedded in title — must not panic.
	task.Title = "Legitimate title\x00suffix"
	_ = task.Validate()
}

// ==================== SQL-injection-pattern strings stored safely ====================

func TestBoundary_SQLInjection_TitleInCreate(t *testing.T) {
	const roadmap = "boundary-sqlinj-create"
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	injectionTitles := []string{
		"'; DROP TABLE tasks; --",
		`" OR "1"="1`,
		"Robert'); DROP TABLE tasks;--",
		"1; SELECT * FROM tasks WHERE 1=1--",
		"' UNION SELECT null,null,null--",
	}

	for _, title := range injectionTitles {
		title := title
		label := title
		if len(label) > 20 {
			label = label[:20]
		}
		t.Run("inject_"+label, func(t *testing.T) {
			out := captureOutput(t, func() {
				if err := HandleTask([]string{
					"create",
					"-r", roadmap,
					"-t", title,
					"-fr", "SQL injection test: functional requirements remain intact",
					"-tr", "Parameterized queries prevent SQL injection by design",
					"-ac", "Database schema is unmodified after insertion",
					"-p", "5",
				}); err != nil {
					t.Errorf("taskCreate with SQL-injection title %q error = %v", title, err)
				}
			})

			obj := parseJSONObject(t, out)
			taskID := int(obj["id"].(float64))
			task, err := database.GetTask(context.Background(), taskID)
			if err != nil {
				t.Fatalf("GetTask(%d) after SQL-injection title: %v", taskID, err)
			}
			if task.Title != title {
				t.Errorf("stored title = %q, want %q (injection mutated the value)", task.Title, title)
			}
		})
	}
}

func TestBoundary_SQLInjection_FunctionalRequirements(t *testing.T) {
	const roadmap = "boundary-sqlinj-func"
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	injectionFR := "' OR '1'='1'; INSERT INTO tasks (title) VALUES ('hacked'); --"

	out := captureOutput(t, func() {
		if err := HandleTask([]string{
			"create",
			"-r", roadmap,
			"-t", "Validate parameterized query security in functional requirements",
			"-fr", injectionFR,
			"-tr", "All database queries use parameterized placeholders",
			"-ac", "Schema is intact and no spurious rows appear after injection attempt",
			"-p", "8",
		}); err != nil {
			t.Errorf("taskCreate with SQL-injection FR error = %v", err)
		}
	})

	taskID := extractIntID(t, out)
	task, err := database.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("GetTask after SQL-injection FR: %v", err)
	}
	if task.FunctionalRequirements != injectionFR {
		t.Errorf("stored FR = %q, want %q", task.FunctionalRequirements, injectionFR)
	}
}

func TestBoundary_SQLInjection_SprintDescription(t *testing.T) {
	const roadmap = "boundary-sqlinj-sprint"
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()

	injectionDesc := "Sprint Alpha'); DROP TABLE sprints; --"

	out := captureOutput(t, func() {
		if err := HandleSprint([]string{
			"create",
			"-r", roadmap,
			"-d", injectionDesc,
		}); err != nil {
			t.Errorf("sprintCreate with SQL-injection description error = %v", err)
		}
	})

	obj := parseJSONObject(t, out)
	sprintID := int(obj["id"].(float64))
	sprint, err := database.GetSprint(context.Background(), sprintID)
	if err != nil {
		t.Fatalf("GetSprint after SQL-injection description: %v", err)
	}
	if sprint.Description != injectionDesc {
		t.Errorf("stored description = %q, want %q", sprint.Description, injectionDesc)
	}
}

// ==================== HandleTask CLI — priority/severity out-of-range ====================

func TestHandleTask_Create_Priority_OutOfRange_Negative(t *testing.T) {
	const roadmap = "boundary-prio-neg"
	_, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()
	err := HandleTask([]string{
		"create",
		"-r", roadmap,
		"-t", "Validate priority lower bound rejection",
		"-fr", "System must reject negative priority values",
		"-tr", "Input validation occurs before database insertion",
		"-ac", "Error returned for priority -1",
		"-p", "-1",
	})
	if err == nil {
		t.Error("priority -1 via CLI should return error, got nil")
	}
}

func TestHandleTask_Create_Priority_OutOfRange_TooHigh(t *testing.T) {
	const roadmap = "boundary-prio-high"
	_, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()
	err := HandleTask([]string{
		"create",
		"-r", roadmap,
		"-t", "Validate priority upper bound rejection",
		"-fr", "System must reject priority values above 9",
		"-tr", "Input validation occurs before database insertion",
		"-ac", "Error returned for priority 10",
		"-p", "10",
	})
	if err == nil {
		t.Error("priority 10 via CLI should return error, got nil")
	}
}

func TestHandleTask_Create_Severity_OutOfRange_Negative(t *testing.T) {
	const roadmap = "boundary-sev-neg"
	_, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()
	err := HandleTask([]string{
		"create",
		"-r", roadmap,
		"-t", "Validate severity lower bound rejection",
		"-fr", "System must reject negative severity values",
		"-tr", "Input validation occurs before database insertion",
		"-ac", "Error returned for severity -1",
		"--severity", "-1",
	})
	if err == nil {
		t.Error("severity -1 via CLI should return error, got nil")
	}
}

func TestHandleTask_Create_Severity_OutOfRange_TooHigh(t *testing.T) {
	const roadmap = "boundary-sev-high"
	_, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()
	err := HandleTask([]string{
		"create",
		"-r", roadmap,
		"-t", "Validate severity upper bound rejection",
		"-fr", "System must reject severity values above 9",
		"-tr", "Input validation occurs before database insertion",
		"-ac", "Error returned for severity 10",
		"--severity", "10",
	})
	if err == nil {
		t.Error("severity 10 via CLI should return error, got nil")
	}
}

// ==================== HandleTask CLI — title max-length enforcement ====================

func TestHandleTask_Create_Title_ExactMaxLength(t *testing.T) {
	const roadmap = "boundary-title-exact"
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()
	exactTitle := repeatByte('x', models.MaxTaskTitle)
	out := captureOutput(t, func() {
		if err := HandleTask([]string{
			"create",
			"-r", roadmap,
			"-t", exactTitle,
			"-fr", "Title at exact maximum byte boundary must be accepted",
			"-tr", "String length validation uses byte count consistent with len() in Go",
			"-ac", "Task is created and title stored verbatim",
		}); err != nil {
			t.Errorf("taskCreate with exact-max title error = %v", err)
		}
	})
	taskID := extractIntID(t, out)
	task, err := database.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("GetTask for exact-max-length title: %v", err)
	}
	if len(task.Title) != models.MaxTaskTitle {
		t.Errorf("stored title byte length = %d, want %d", len(task.Title), models.MaxTaskTitle)
	}
}

func TestHandleTask_Create_Title_OneBeyondMaxLength(t *testing.T) {
	const roadmap = "boundary-title-over"
	_, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()
	overTitle := repeatByte('x', models.MaxTaskTitle+1)
	err := HandleTask([]string{
		"create",
		"-r", roadmap,
		"-t", overTitle,
		"-fr", "Title one byte over maximum must be rejected",
		"-tr", "Validation rejects the payload before reaching the database",
		"-ac", "Error returned and no task row inserted",
	})
	if err == nil {
		t.Errorf("title of %d bytes (max+1) should be rejected, got nil", models.MaxTaskTitle+1)
	}
}

// ==================== HandleTask CLI — Unicode round-trip ====================

func TestHandleTask_Create_Unicode_CJKTitle_RoundTrip(t *testing.T) {
	const roadmap = "boundary-unicode-cjk"
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()
	// 50 CJK characters = 150 UTF-8 bytes — well within 255-byte limit.
	unicodeTitle := strings.Repeat("実装", 25)
	unicodeFR := "実装チームによる認証サービスのデプロイメント要件"
	out := captureOutput(t, func() {
		if err := HandleTask([]string{
			"create",
			"-r", roadmap,
			"-t", unicodeTitle,
			"-fr", unicodeFR,
			"-tr", "SQLite stores UTF-8 natively; no transcoding layer required",
			"-ac", "Retrieved title and description match the original Unicode input verbatim",
			"-p", "7",
		}); err != nil {
			t.Errorf("taskCreate with CJK title error = %v", err)
		}
	})
	taskID := extractIntID(t, out)
	task, err := database.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("GetTask for CJK title: %v", err)
	}
	if task.Title != unicodeTitle {
		t.Errorf("CJK title round-trip failed: stored %q, want %q", task.Title, unicodeTitle)
	}
	if task.FunctionalRequirements != unicodeFR {
		t.Errorf("CJK functional_requirements round-trip failed: stored %q, want %q",
			task.FunctionalRequirements, unicodeFR)
	}
}

func TestHandleTask_Create_Unicode_AccentedTitle_RoundTrip(t *testing.T) {
	const roadmap = "boundary-unicode-accent"
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()
	accentTitle := "Implementação do serviço de autenticação OAuth2"
	out := captureOutput(t, func() {
		if err := HandleTask([]string{
			"create",
			"-r", roadmap,
			"-t", accentTitle,
			"-fr", "Permitir autenticação via fornecedor SSO corporativo",
			"-tr", "Integrar Auth0 SDK; actualizar middleware de sessão",
			"-ac", "Utilizadores autenticam via SSO sem necessidade de reset de password",
			"-p", "6",
		}); err != nil {
			t.Errorf("taskCreate with accented title error = %v", err)
		}
	})
	taskID := extractIntID(t, out)
	task, err := database.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("GetTask for accented title: %v", err)
	}
	if task.Title != accentTitle {
		t.Errorf("accented title round-trip failed: stored %q, want %q", task.Title, accentTitle)
	}
}

func TestHandleTask_Create_Unicode_RTLTitle_RoundTrip(t *testing.T) {
	const roadmap = "boundary-unicode-rtl"
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()
	rtlTitle := "تنفيذ بوابة الدفع الآمنة"
	out := captureOutput(t, func() {
		if err := HandleTask([]string{
			"create",
			"-r", roadmap,
			"-t", rtlTitle,
			"-fr", "Enable secure payment processing for Arabic-locale customers",
			"-tr", "Integrate Stripe SDK with locale-aware error messages",
			"-ac", "Payment flow completes without data corruption for RTL inputs",
			"-p", "8",
		}); err != nil {
			t.Errorf("taskCreate with RTL title error = %v", err)
		}
	})
	taskID := extractIntID(t, out)
	task, err := database.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("GetTask for RTL title: %v", err)
	}
	if task.Title != rtlTitle {
		t.Errorf("RTL title round-trip failed: stored %q, want %q", task.Title, rtlTitle)
	}
}

// ==================== HandleTask CLI — 4096-char field round-trips ====================

func TestHandleTask_Create_LongFunctionalRequirements_ExactMax(t *testing.T) {
	const roadmap = "boundary-fr-exact"
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()
	exactFR := repeatByte('f', models.MaxTaskFunctionalRequirements)
	out := captureOutput(t, func() {
		if err := HandleTask([]string{
			"create",
			"-r", roadmap,
			"-t", "Verify max-length functional requirements are accepted",
			"-fr", exactFR,
			"-tr", "String length validation uses len() byte count",
			"-ac", "Task is created and functional requirements stored intact",
		}); err != nil {
			t.Errorf("taskCreate with exact-max FR error = %v", err)
		}
	})
	taskID := extractIntID(t, out)
	task, err := database.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("GetTask for exact-max FR: %v", err)
	}
	if len(task.FunctionalRequirements) != models.MaxTaskFunctionalRequirements {
		t.Errorf("stored FR byte length = %d, want %d",
			len(task.FunctionalRequirements), models.MaxTaskFunctionalRequirements)
	}
}

func TestHandleTask_Create_LongFunctionalRequirements_OneBeyondMax(t *testing.T) {
	const roadmap = "boundary-fr-over"
	_, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()
	overFR := repeatByte('f', models.MaxTaskFunctionalRequirements+1)
	err := HandleTask([]string{
		"create",
		"-r", roadmap,
		"-t", "Verify over-max functional requirements are rejected",
		"-fr", overFR,
		"-tr", "Validation rejects input before database insertion",
		"-ac", "Error returned and no task row inserted",
	})
	if err == nil {
		t.Errorf("FR of %d bytes (max+1) should be rejected, got nil",
			models.MaxTaskFunctionalRequirements+1)
	}
}

// ==================== DB-level priority/severity boundary confirmation ====================

func TestBoundary_DB_Priority_MinBoundary(t *testing.T) {
	const roadmap = "boundary-db-prio-min"
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()
	out := captureOutput(t, func() {
		if err := HandleTask([]string{
			"create",
			"-r", roadmap,
			"-t", "Verify minimum priority stored correctly in the database",
			"-fr", "Priority 0 must be persisted and retrieved without loss",
			"-tr", "Database stores integer value 0 for priority column",
			"-ac", "GetTask returns priority == 0 after creation with priority 0",
			"-p", "0",
		}); err != nil {
			t.Fatalf("taskCreate with priority 0 error = %v", err)
		}
	})
	taskID := extractIntID(t, out)
	task, err := database.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("GetTask for min-priority task: %v", err)
	}
	if task.Priority != 0 {
		t.Errorf("DB priority = %d, want 0", task.Priority)
	}
}

func TestBoundary_DB_Priority_MaxBoundary(t *testing.T) {
	const roadmap = "boundary-db-prio-max"
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()
	out := captureOutput(t, func() {
		if err := HandleTask([]string{
			"create",
			"-r", roadmap,
			"-t", "Verify maximum priority stored correctly in the database",
			"-fr", "Priority 9 must be persisted and retrieved without loss",
			"-tr", "Database stores integer value 9 for priority column",
			"-ac", "GetTask returns priority == 9 after creation with priority 9",
			"-p", "9",
		}); err != nil {
			t.Fatalf("taskCreate with priority 9 error = %v", err)
		}
	})
	taskID := extractIntID(t, out)
	task, err := database.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("GetTask for max-priority task: %v", err)
	}
	if task.Priority != 9 {
		t.Errorf("DB priority = %d, want 9", task.Priority)
	}
}

func TestBoundary_DB_Severity_MinBoundary(t *testing.T) {
	const roadmap = "boundary-db-sev-min"
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()
	out := captureOutput(t, func() {
		if err := HandleTask([]string{
			"create",
			"-r", roadmap,
			"-t", "Verify minimum severity stored correctly in the database",
			"-fr", "Severity 0 must be persisted and retrieved without loss",
			"-tr", "Database stores integer value 0 for severity column",
			"-ac", "GetTask returns severity == 0 after creation with severity 0",
			"--severity", "0",
		}); err != nil {
			t.Fatalf("taskCreate with severity 0 error = %v", err)
		}
	})
	taskID := extractIntID(t, out)
	task, err := database.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("GetTask for min-severity task: %v", err)
	}
	if task.Severity != 0 {
		t.Errorf("DB severity = %d, want 0", task.Severity)
	}
}

func TestBoundary_DB_Severity_MaxBoundary(t *testing.T) {
	const roadmap = "boundary-db-sev-max"
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()
	out := captureOutput(t, func() {
		if err := HandleTask([]string{
			"create",
			"-r", roadmap,
			"-t", "Verify maximum severity stored correctly in the database",
			"-fr", "Severity 9 must be persisted and retrieved without loss",
			"-tr", "Database stores integer value 9 for severity column",
			"-ac", "GetTask returns severity == 9 after creation with severity 9",
			"--severity", "9",
		}); err != nil {
			t.Fatalf("taskCreate with severity 9 error = %v", err)
		}
	})
	taskID := extractIntID(t, out)
	task, err := database.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("GetTask for max-severity task: %v", err)
	}
	if task.Severity != 9 {
		t.Errorf("DB severity = %d, want 9", task.Severity)
	}
}

// ==================== DB-level UpdateTaskPriority/UpdateTaskSeverity boundary ====================

func TestBoundary_DB_UpdateTaskPriority_MinBoundary(t *testing.T) {
	const roadmap = "boundary-db-upd-prio-min"
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()
	out := captureOutput(t, func() {
		if err := HandleTask([]string{
			"create",
			"-r", roadmap,
			"-t", "Verify UpdateTaskPriority accepts minimum value 0",
			"-fr", "Priority can be updated to 0 after task creation",
			"-tr", "UpdateTaskPriority issues parameterized SQL UPDATE",
			"-ac", "Subsequent GetTask returns priority == 0",
			"-p", "5",
		}); err != nil {
			t.Fatalf("create task error: %v", err)
		}
	})
	taskID := extractIntID(t, out)
	if err := database.UpdateTaskPriority(context.Background(), []int{taskID}, 0); err != nil {
		t.Fatalf("UpdateTaskPriority(0) error = %v", err)
	}
	task, err := database.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("GetTask after priority update: %v", err)
	}
	if task.Priority != 0 {
		t.Errorf("priority after update to 0 = %d, want 0", task.Priority)
	}
}

func TestBoundary_DB_UpdateTaskPriority_MaxBoundary(t *testing.T) {
	const roadmap = "boundary-db-upd-prio-max"
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()
	out := captureOutput(t, func() {
		if err := HandleTask([]string{
			"create",
			"-r", roadmap,
			"-t", "Verify UpdateTaskPriority accepts maximum value 9",
			"-fr", "Priority can be updated to 9 after task creation",
			"-tr", "UpdateTaskPriority issues parameterized SQL UPDATE",
			"-ac", "Subsequent GetTask returns priority == 9",
			"-p", "0",
		}); err != nil {
			t.Fatalf("create task error: %v", err)
		}
	})
	taskID := extractIntID(t, out)
	if err := database.UpdateTaskPriority(context.Background(), []int{taskID}, 9); err != nil {
		t.Fatalf("UpdateTaskPriority(9) error = %v", err)
	}
	task, err := database.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("GetTask after priority update: %v", err)
	}
	if task.Priority != 9 {
		t.Errorf("priority after update to 9 = %d, want 9", task.Priority)
	}
}

func TestBoundary_DB_UpdateTaskSeverity_MinBoundary(t *testing.T) {
	const roadmap = "boundary-db-upd-sev-min"
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()
	out := captureOutput(t, func() {
		if err := HandleTask([]string{
			"create",
			"-r", roadmap,
			"-t", "Verify UpdateTaskSeverity accepts minimum value 0",
			"-fr", "Severity can be updated to 0 after task creation",
			"-tr", "UpdateTaskSeverity issues parameterized SQL UPDATE",
			"-ac", "Subsequent GetTask returns severity == 0",
			"--severity", "5",
		}); err != nil {
			t.Fatalf("create task error: %v", err)
		}
	})
	taskID := extractIntID(t, out)
	if err := database.UpdateTaskSeverity(context.Background(), []int{taskID}, 0); err != nil {
		t.Fatalf("UpdateTaskSeverity(0) error = %v", err)
	}
	task, err := database.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("GetTask after severity update: %v", err)
	}
	if task.Severity != 0 {
		t.Errorf("severity after update to 0 = %d, want 0", task.Severity)
	}
}

func TestBoundary_DB_UpdateTaskSeverity_MaxBoundary(t *testing.T) {
	const roadmap = "boundary-db-upd-sev-max"
	database, cleanup := setupTestTaskRoadmap(t, roadmap)
	defer cleanup()
	out := captureOutput(t, func() {
		if err := HandleTask([]string{
			"create",
			"-r", roadmap,
			"-t", "Verify UpdateTaskSeverity accepts maximum value 9",
			"-fr", "Severity can be updated to 9 after task creation",
			"-tr", "UpdateTaskSeverity issues parameterized SQL UPDATE",
			"-ac", "Subsequent GetTask returns severity == 9",
			"--severity", "0",
		}); err != nil {
			t.Fatalf("create task error: %v", err)
		}
	})
	taskID := extractIntID(t, out)
	if err := database.UpdateTaskSeverity(context.Background(), []int{taskID}, 9); err != nil {
		t.Fatalf("UpdateTaskSeverity(9) error = %v", err)
	}
	task, err := database.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("GetTask after severity update: %v", err)
	}
	if task.Severity != 9 {
		t.Errorf("severity after update to 9 = %d, want 9", task.Severity)
	}
}
