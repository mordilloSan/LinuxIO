package updates

import (
	"bufio"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/mordilloSan/go_logger/logger"

	"github.com/gin-gonic/gin"
)

func getUpdateHistoryHandler(c *gin.Context) {
	history := parseUpdateHistory()
	if len(history) == 0 {
		logger.Warnf("No update history found")
		c.JSON(http.StatusNotFound, gin.H{"error": "No update history found"})
		return
	}
	c.JSON(http.StatusOK, history)
}

func parseUpdateHistory() []UpdateHistoryEntry {
	if _, err := os.Stat("/var/log/dpkg.log"); err == nil {
		logger.Infof("Parsing dpkg update history")
		return parseDpkgLog("/var/log/dpkg.log")
	}
	if _, err := os.Stat("/var/log/dnf.log"); err == nil {
		logger.Infof("Parsing dnf update history")
		return parseDnfHistory("/var/log/dnf.log")
	}
	logger.Warnf("No known package manager log found")
	return []UpdateHistoryEntry{}
}

func parseDpkgLog(logPath string) []UpdateHistoryEntry {
	file, err := os.Open(logPath)
	if err != nil {
		logger.Errorf("Failed to open dpkg log: %v", err)
		return nil
	}
	defer func() {
		if cerr := file.Close(); cerr != nil {
			logger.Warnf("failed to close dpkg log file: %v", cerr)
		}
	}()

	scanner := bufio.NewScanner(file)

	installRe := regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})\s+\d{2}:\d{2}:\d{2}\s+(install|upgrade)\s+([^ ]+)\s+([^ ]+)`)
	configureRe := regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})\s+\d{2}:\d{2}:\d{2}\s+configure\s+([^ ]+)\s+([^ ]+)`)

	historyMap := make(map[string][]UpgradeItem)
	pendingPackages := make(map[string]string)

	for scanner.Scan() {
		line := scanner.Text()

		if matches := installRe.FindStringSubmatch(line); len(matches) == 5 {
			date, pkg, version := matches[1], matches[3], matches[4]
			if version == "<none>" {
				pendingPackages[pkg] = date
			} else {
				historyMap[date] = append(historyMap[date], UpgradeItem{
					Package: pkg,
					Version: version,
				})
			}
		}

		if matches := configureRe.FindStringSubmatch(line); len(matches) == 4 {
			_, pkg, version := matches[1], matches[2], matches[3]
			if origDate, exists := pendingPackages[pkg]; exists {
				historyMap[origDate] = append(historyMap[origDate], UpgradeItem{
					Package: pkg,
					Version: version,
				})
				delete(pendingPackages, pkg)
			}
		}

	}

	return mapToSortedHistory(historyMap)
}

func parseDnfHistory(logPath string) []UpdateHistoryEntry {
	file, err := os.Open(logPath)
	if err != nil {
		logger.Errorf("Failed to open DNF log: %v", err)
		return nil
	}
	defer func() {
		if cerr := file.Close(); cerr != nil {
			logger.Warnf("failed to close DNF log file: %v", cerr)
		}
	}()

	scanner := bufio.NewScanner(file)
	upgradeRe := regexp.MustCompile(`Upgrade:\s+([^\s-]+)-([^-]+-[^\s]+)`)

	historyMap := make(map[string][]UpgradeItem)

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) < 1 {
			continue
		}
		date := parts[0]

		if matches := upgradeRe.FindStringSubmatch(line); len(matches) > 2 {
			historyMap[date] = append(historyMap[date], UpgradeItem{
				Package: matches[1],
				Version: matches[2],
			})
		}
	}

	return mapToSortedHistory(historyMap)
}

func mapToSortedHistory(historyMap map[string][]UpgradeItem) []UpdateHistoryEntry {
	var dates []string
	for date := range historyMap {
		dates = append(dates, date)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))

	var history []UpdateHistoryEntry
	for _, date := range dates {
		history = append(history, UpdateHistoryEntry{
			Date:     date,
			Upgrades: historyMap[date],
		})
	}
	return history
}
