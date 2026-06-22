package cron

import (
	"fmt"
	"os/exec"
	"strings"
)

// ScheduleBackup añade o actualiza un cronjob para la instancia
func ScheduleBackup(instanceID string, executablePath string, hours int) error {
	RemoveBackup(instanceID) // Quitar previo si existe

	cmd := exec.Command("crontab", "-l")
	out, _ := cmd.Output() // Ignoramos el error si crontab está vacío

	currentCron := string(out)
	
	// Crear la nueva regla
	// "0 */X * * *" ejecuta cada X horas en el minuto 0
	marker := fmt.Sprintf(" # LAZYBLOCKS_BACKUP_%s", instanceID)
	newRule := fmt.Sprintf("0 */%d * * * %s crond %s%s\n", hours, executablePath, instanceID, marker)

	newCron := currentCron
	if !strings.HasSuffix(newCron, "\n") && newCron != "" {
		newCron += "\n"
	}
	newCron += newRule

	// Instalar el nuevo crontab
	updateCmd := exec.Command("crontab", "-")
	updateCmd.Stdin = strings.NewReader(newCron)
	if err := updateCmd.Run(); err != nil {
		return fmt.Errorf("failed to install cronjob: %w", err)
	}

	return nil
}

// RemoveBackup quita cualquier cronjob asociado a la instancia
func RemoveBackup(instanceID string) error {
	cmd := exec.Command("crontab", "-l")
	out, err := cmd.Output()
	if err != nil {
		return nil // Probablemente no hay crontab instalado
	}

	marker := fmt.Sprintf(" # LAZYBLOCKS_BACKUP_%s", instanceID)
	lines := strings.Split(string(out), "\n")
	var newLines []string

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.Contains(line, marker) {
			newLines = append(newLines, line)
		}
	}

	newCron := strings.Join(newLines, "\n") + "\n"
	
	updateCmd := exec.Command("crontab", "-")
	updateCmd.Stdin = strings.NewReader(newCron)
	return updateCmd.Run()
}
