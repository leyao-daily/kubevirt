/*
 * This file is part of the KubeVirt project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright 2018 Red Hat, Inc.
 *
 */

 package prometheus

 import (
	 "fmt"
	 "net/http"
	 "strings"
	 "time"
 
	 "github.com/prometheus/client_golang/prometheus"
	 "github.com/prometheus/client_golang/prometheus/promhttp"
 
	 k6tv1 "kubevirt.io/client-go/api/v1"
	 "kubevirt.io/client-go/kubecli"
	 "kubevirt.io/client-go/log"
	 "kubevirt.io/client-go/version"
	 "kubevirt.io/kubevirt/pkg/util/lookup"
	 cmdclient "kubevirt.io/kubevirt/pkg/virt-handler/cmd-client"
	 "kubevirt.io/kubevirt/pkg/virt-launcher/virtwrap/stats"
 )
 
 const statsMaxAge time.Duration = collectionTimeout + 2*time.Second // "a bit more" than timeout, heuristic again
 
 var (
	 // Formatter used to sanitize k8s metadata into metric labels
	 labelFormatter = strings.NewReplacer(".", "_", "/", "_", "-", "_")
 
	 // see https://www.robustperception.io/exposing-the-software-version-to-prometheus
	 versionDesc = prometheus.NewDesc(
		 "kubevirt_info",
		 "Version information",
		 []string{"goversion", "kubeversion"},
		 nil,
	 )
 
	 // higher-level, telemetry-friendly metrics
	 vmiCountDesc = prometheus.NewDesc(
		 "kubevirt_vmi_phase_count",
		 "VMI phase.",
		 []string{
			 "node", "phase",
		 },
		 nil,
	 )
 
	 // lower level metrics
	 storageIopsLabels = []string{"node", "namespace", "name", "drive", "type"}
	 storageIopsDesc = prometheus.NewDesc(
		 "kubevirt_vmi_storage_iops_total",
		 "I/O operation performed.",
		 storageIopsLabels,
		 nil,
	 )
 
	 storageTrafficLabels = []string{"node", "namespace", "name", "drive", "type"}
	 storageTrafficDesc = prometheus.NewDesc(
		 "kubevirt_vmi_storage_traffic_bytes_total",
		 "storage traffic.",
		 storageTrafficLabels,
		 nil,
	 )
 
	 storageTimesLabels = []string{"node", "namespace", "name", "drive", "type"}
	 storageTimesDesc = prometheus.NewDesc(
		 "kubevirt_vmi_storage_times_ms_total",
		 "storage operation time.",
		 storageTimesLabels,
		 nil,
	 )
 
	 vcpuUsageLabels = []string{"node", "namespace", "name", "id", "state"}
	 vcpuUsageDesc = prometheus.NewDesc(
		 "kubevirt_vmi_vcpu_seconds",
		 "Vcpu elapsed time.",
		 vcpuUsageLabels,
		 nil,
	 )
 
	 networkTrafficBytesLabels = []string{"node", "namespace", "name", "interface", "type"}
	 networkTrafficBytesDesc = prometheus.NewDesc(
		 "kubevirt_vmi_network_traffic_bytes_total",
		 "network traffic.",
		 networkTrafficBytesLabels,
		 nil,
	 )
 
	 networkTrafficPktsLabels = []string{"node", "namespace", "name", "interface", "type"}
	 networkTrafficPktsDesc = prometheus.NewDesc(
		 "kubevirt_vmi_network_traffic_packets_total",
		 "network traffic.",
		 networkTrafficPktsLabels,
		 nil,
	 )
 
	 networkErrorsLabels = []string{"node", "namespace", "name", "interface", "type"}
	 networkErrorsDesc = prometheus.NewDesc(
		 "kubevirt_vmi_network_errors_total",
		 "network errors.",
		 networkErrorsLabels,
		 nil,
	 )
 
	 memoryAvailableLabels = []string{"node", "namespace", "name"}
	 memoryAvailableDesc = prometheus.NewDesc(
		 "kubevirt_vmi_memory_available_bytes",
		 "amount of usable memory as seen by the domain.",
		 memoryAvailableLabels,
		 nil,
	 )
 
	 memoryResidentLabels = []string{"node", "namespace", "name"}
	 memoryResidentDesc = prometheus.NewDesc(
		 "kubevirt_vmi_memory_resident_bytes",
		 "resident set size of the process running the domain",
		 memoryResidentLabels,
		 nil,
	 )
 
	 swapTrafficLabels = []string{"node", "namespace", "name", "type"}
	 swapTrafficDesc = prometheus.NewDesc(
		 "kubevirt_vmi_memory_swap_traffic_bytes_total",
		 "swap memory traffic.",
		 swapTrafficLabels,
		 nil,
	 )
 )
 
 func tryToPushMetric(desc *prometheus.Desc, mv prometheus.Metric, err error, ch chan<- prometheus.Metric) {
	 if err != nil {
		 log.Log.V(4).Warningf("Error creating the new const metric for %s: %s", memoryAvailableDesc, err)
		 return
	 }
	 ch <- mv
 }
 
 func updateMemory(vmi *k6tv1.VirtualMachineInstance, vmStats *stats.DomainStats, ch chan<- prometheus.Metric) {
	 memoryResidentLabels = []string{"node", "namespace", "name"}
	 memoryAvailableLabels = []string{"node", "namespace", "name"}
	 swapTrafficLabels = []string{"node", "namespace", "name", "type"}
	 var memoryResidentLabelValues = []string{vmi.Status.NodeName, vmi.Namespace, vmi.Name}
	 var memoryAvailableLabelsValues = []string{vmi.Status.NodeName, vmi.Namespace, vmi.Name}
	 var swapTrafficInLabelsValues = []string{vmi.Status.NodeName, vmi.Namespace, vmi.Name, "in"}
	 var swapTrafficOutLabelsValues = []string{vmi.Status.NodeName, vmi.Namespace, vmi.Name, "out"}
 
	 // Add k8s metadata.Labels as metric labels
	 labelPreffix := "vmi_k8s_label_"
	 for label, val := range vmi.Labels {
		 memoryResidentLabels = append(memoryResidentLabels, labelPreffix+labelFormatter.Replace(label))
		 memoryResidentLabelValues = append(memoryResidentLabelValues, val)
 
		 memoryAvailableLabels = append(memoryAvailableLabels, labelPreffix+labelFormatter.Replace(label))
		 memoryAvailableLabelsValues = append(memoryAvailableLabelsValues, val)
 
		 swapTrafficLabels = append(swapTrafficLabels, labelPreffix+labelFormatter.Replace(label))
		 swapTrafficInLabelsValues = append(swapTrafficInLabelsValues, val)
		 swapTrafficOutLabelsValues = append(swapTrafficOutLabelsValues, val)
	 }
 
	 // Add k8s metadata.Annotations as metric labels
	 annotationPreffix := "vmi_k8s_annotation_"
	 for annotation, val := range vmi.Annotations {
		 memoryResidentLabels = append(memoryResidentLabels, annotationPreffix+labelFormatter.Replace(annotation))
		 memoryResidentLabelValues = append(memoryResidentLabelValues, val)
 
		 memoryAvailableLabels = append(memoryAvailableLabels, annotationPreffix+labelFormatter.Replace(annotation))
		 memoryAvailableLabelsValues = append(memoryAvailableLabelsValues, val)
 
		 swapTrafficLabels = append(swapTrafficLabels, annotationPreffix+labelFormatter.Replace(annotation))
		 swapTrafficInLabelsValues = append(swapTrafficInLabelsValues, val)
		 swapTrafficOutLabelsValues = append(swapTrafficOutLabelsValues, val)
	 }
 
 
	 memoryResidentDesc = prometheus.NewDesc(
		 "kubevirt_vmi_memory_resident_bytes",
		 "resident set size of the process running the domain",
		 memoryResidentLabels,
		 nil,
	 )
 
	 memoryAvailableDesc = prometheus.NewDesc(
		 "kubevirt_vmi_memory_available_bytes",
		 "amount of usable memory as seen by the domain.",
		 memoryAvailableLabels,
		 nil,
	 )
 
	 swapTrafficDesc = prometheus.NewDesc(
		 "kubevirt_vmi_memory_swap_traffic_bytes_total",
		 "swap memory traffic.",
		 swapTrafficLabels,
		 nil,
	 )
 
	 if vmStats.Memory.RSSSet {
		 mv, err := prometheus.NewConstMetric(
			 memoryResidentDesc, prometheus.GaugeValue,
			 // the libvirt value is in KiB
			 float64(vmStats.Memory.RSS)*1024,
			 memoryResidentLabelValues...
		 )
		 tryToPushMetric(memoryResidentDesc, mv, err, ch)
	 }
 
	 if vmStats.Memory.AvailableSet {
		 mv, err := prometheus.NewConstMetric(
			 memoryAvailableDesc, prometheus.GaugeValue,
			 // the libvirt value is in KiB
			 float64(vmStats.Memory.Available)*1024,
			 memoryAvailableLabelsValues...
		 )
		 tryToPushMetric(memoryAvailableDesc, mv, err, ch)
	 }
 
 
	 if vmStats.Memory.SwapInSet {
		 mv, err := prometheus.NewConstMetric(
			 swapTrafficDesc, prometheus.GaugeValue,
			 // the libvirt value is in KiB
			 float64(vmStats.Memory.SwapIn)*1024,
			 swapTrafficInLabelsValues...
		 )
		 tryToPushMetric(swapTrafficDesc, mv, err, ch)
	 }
	 if vmStats.Memory.SwapOutSet {
		 mv, err := prometheus.NewConstMetric(
			 swapTrafficDesc, prometheus.GaugeValue,
			 // the libvirt value is in KiB
			 float64(vmStats.Memory.SwapOut)*1024,
			 swapTrafficOutLabelsValues...
		 )
		 tryToPushMetric(swapTrafficDesc, mv, err, ch)
	 }
 }
 
 func updateVcpu(vmi *k6tv1.VirtualMachineInstance, vmStats *stats.DomainStats, ch chan<- prometheus.Metric) {
	 for vcpuId, vcpu := range vmStats.Vcpu {
		 vcpuUsageLabels = []string{"node", "namespace", "name", "id", "state"}
		 var vcpuUsageLabelsValues = []string{vmi.Status.NodeName, vmi.Namespace, vmi.Name, fmt.Sprintf("%v", vcpuId), fmt.Sprintf("%v", vcpu.State)}
 
		 // Add k8s metadata.Labels as metric labels
		 labelPreffix := "vmi_k8s_label_"
		 for label, val := range vmi.Labels {
			 vcpuUsageLabels = append(vcpuUsageLabels, labelPreffix+labelFormatter.Replace(label))
			 vcpuUsageLabelsValues = append(vcpuUsageLabelsValues, val)
		 }
 
		 // Add k8s metadata.Annotations as metric labels
		 annotationPreffix := "vmi_k8s_annotation_"
		 for annotation, val := range vmi.Annotations {
			 vcpuUsageLabels = append(vcpuUsageLabels, annotationPreffix+labelFormatter.Replace(annotation))
			 vcpuUsageLabelsValues = append(vcpuUsageLabelsValues, val)
		 }
 
		 vcpuUsageDesc = prometheus.NewDesc(
			 "kubevirt_vmi_vcpu_seconds",
			 "Vcpu elapsed time.",
			 vcpuUsageLabels,
			 nil,
		 )
 
		 if !vcpu.StateSet || !vcpu.TimeSet {
			 log.Log.V(4).Warningf("State or time not set for vcpu#%d", vcpuId)
			 continue
		 }
		 mv, err := prometheus.NewConstMetric(
			 vcpuUsageDesc, prometheus.GaugeValue,
			 float64(vcpu.Time/1000000000),
			 vcpuUsageLabelsValues...
		 )
		 tryToPushMetric(vcpuUsageDesc, mv, err, ch)
	 }
 }
 
 func updateBlock(vmi *k6tv1.VirtualMachineInstance, vmStats *stats.DomainStats, ch chan<- prometheus.Metric) {
	 for blockId, block := range vmStats.Block {
		 storageIopsLabels = []string{"node", "namespace", "name", "drive", "type"}
		 storageTrafficLabels = []string{"node", "namespace", "name", "drive", "type"}
		 storageTimesLabels = []string{"node", "namespace", "name", "drive", "type"}
		 var storageIopsReadLabelsValues = []string{vmi.Status.NodeName, vmi.Namespace, vmi.Name, block.Name, "read"}
		 var storageIopsWriteLabelsValues = []string{vmi.Status.NodeName, vmi.Namespace, vmi.Name, block.Name, "write"}
		 var storageTrafficReadLabelsValues = []string{vmi.Status.NodeName, vmi.Namespace, vmi.Name, block.Name, "read"}
		 var storageTrafficWriteLabelsValues = []string{vmi.Status.NodeName, vmi.Namespace, vmi.Name, block.Name, "write"}
		 var storageTimesReadLabelsValues = []string{vmi.Status.NodeName, vmi.Namespace, vmi.Name, block.Name, "read"}
		 var storageTimesWriteLabelsValues = []string{vmi.Status.NodeName, vmi.Namespace, vmi.Name, block.Name, "write"}
 
		 // Add k8s metadata.Labels as metric labels
		 labelPreffix := "vmi_k8s_label_"
		 for label, val := range vmi.Labels {
			 storageIopsLabels = append(storageIopsLabels, labelPreffix+labelFormatter.Replace(label))
			 storageIopsReadLabelsValues = append(storageIopsReadLabelsValues, val)
			 storageIopsWriteLabelsValues = append(storageIopsReadLabelsValues, val)
 
			 storageTrafficLabels = append(storageTrafficLabels, labelPreffix+labelFormatter.Replace(label))
			 storageTrafficReadLabelsValues = append(storageTrafficReadLabelsValues, val)
			 storageTrafficWriteLabelsValues = append(storageTrafficWriteLabelsValues, val)
 
			 storageTimesLabels = append(storageTimesLabels, labelPreffix+labelFormatter.Replace(label))
			 storageTimesReadLabelsValues = append(storageTimesReadLabelsValues, val)
			 storageTimesWriteLabelsValues = append(storageTimesWriteLabelsValues, val)
		 }
 
		 // Add k8s metadata.Annotations as metric labels
		 annotationPreffix := "vmi_k8s_annotation_"
		 for annotation, val := range vmi.Annotations {
			 storageIopsLabels = append(storageIopsLabels, annotationPreffix+labelFormatter.Replace(annotation))
			 storageIopsReadLabelsValues = append(storageIopsReadLabelsValues, val)
			 storageIopsWriteLabelsValues = append(storageIopsReadLabelsValues, val)
 
			 storageTrafficLabels = append(storageTrafficLabels, annotationPreffix+labelFormatter.Replace(annotation))
			 storageTrafficReadLabelsValues = append(storageTrafficReadLabelsValues, val)
			 storageTrafficWriteLabelsValues = append(storageTrafficWriteLabelsValues, val)
 
			 storageTimesLabels = append(storageTimesLabels, annotationPreffix+labelFormatter.Replace(annotation))
			 storageTimesReadLabelsValues = append(storageTimesReadLabelsValues, val)
			 storageTimesWriteLabelsValues = append(storageTimesWriteLabelsValues, val)
		 }
 
		 storageIopsDesc = prometheus.NewDesc(
			 "kubevirt_vmi_storage_iops_total",
			 "I/O operation performed.",
			 storageIopsLabels,
			 nil,
		 )
 
		 storageTrafficDesc = prometheus.NewDesc(
			 "kubevirt_vmi_storage_traffic_bytes_total",
			 "storage traffic.",
			 storageTrafficLabels,
			 nil,
		 )
 
		 storageTimesDesc = prometheus.NewDesc(
			 "kubevirt_vmi_storage_times_ms_total",
			 "storage operation time.",
			 storageTimesLabels,
			 nil,
		 )
 
		 if !block.NameSet {
			 log.Log.V(4).Warningf("Name not set for block device#%d", blockId)
			 continue
		 }
 
		 if block.RdReqsSet {
			 mv, err := prometheus.NewConstMetric(
				 storageIopsDesc, prometheus.CounterValue,
				 float64(block.RdReqs),
				 storageIopsReadLabelsValues...
			 )
			 tryToPushMetric(storageIopsDesc, mv, err, ch)
		 }
		 if block.WrReqsSet {
			 mv, err := prometheus.NewConstMetric(
				 storageIopsDesc, prometheus.CounterValue,
				 float64(block.WrReqs),
				 storageIopsWriteLabelsValues...
			 )
			 tryToPushMetric(storageIopsDesc, mv, err, ch)
		 }
 
		 if block.RdBytesSet {
			 mv, err := prometheus.NewConstMetric(
				 storageTrafficDesc, prometheus.CounterValue,
				 float64(block.RdBytes),
				 storageTrafficReadLabelsValues...
			 )
			 tryToPushMetric(storageTrafficDesc, mv, err, ch)
		 }
		 if block.WrBytesSet {
			 mv, err := prometheus.NewConstMetric(
				 storageTrafficDesc, prometheus.CounterValue,
				 float64(block.WrBytes),
				 storageTrafficWriteLabelsValues...
			 )
			 tryToPushMetric(storageTrafficDesc, mv, err, ch)
		 }
 
		 if block.RdTimesSet {
			 mv, err := prometheus.NewConstMetric(
				 storageTimesDesc, prometheus.CounterValue,
				 float64(block.RdTimes),
				 storageTimesReadLabelsValues...
			 )
			 tryToPushMetric(storageTimesDesc, mv, err, ch)
		 }
		 if block.WrTimesSet {
			 mv, err := prometheus.NewConstMetric(
				 storageTimesDesc, prometheus.CounterValue,
				 float64(block.WrTimes),
				 storageTimesWriteLabelsValues...
			 )
			 tryToPushMetric(storageTimesDesc, mv, err, ch)
		 }
	 }
 }
 
 func updateNetwork(vmi *k6tv1.VirtualMachineInstance, vmStats *stats.DomainStats, ch chan<- prometheus.Metric) {
	 for _, net := range vmStats.Net {
		 networkTrafficBytesLabels = []string{"node", "namespace", "name", "interface", "type"}
		 networkTrafficPktsLabels = []string{"node", "namespace", "name", "interface", "type"}
		 networkErrorsLabels = []string{"node", "namespace", "name", "interface", "type"}
		 var networkTrafficBytesRxLabelsValues = []string{vmi.Status.NodeName, vmi.Namespace, vmi.Name, net.Name, "rx",}
		 var networkTrafficBytesTxLabelsValues = []string{vmi.Status.NodeName, vmi.Namespace, vmi.Name, net.Name, "tx",}
		 var networkTrafficPktsRxLabelsValues = []string{vmi.Status.NodeName, vmi.Namespace, vmi.Name, net.Name, "rx",}
		 var networkTrafficPktsTxLabelsValues = []string{vmi.Status.NodeName, vmi.Namespace, vmi.Name, net.Name, "tx",}
		 var networkErrorsRxLabelsValues = []string{vmi.Status.NodeName, vmi.Namespace, vmi.Name, net.Name, "rx",}
		 var networkErrorsTxLabelsValues = []string{vmi.Status.NodeName, vmi.Namespace, vmi.Name, net.Name, "tx",}
 
		 // Add k8s metadata.Labels as metric labels
		 labelPreffix := "vmi_k8s_label_"
		 for label, val := range vmi.Labels {
			 networkTrafficBytesLabels = append(networkTrafficBytesLabels, labelPreffix+labelFormatter.Replace(label))
			 networkTrafficBytesRxLabelsValues = append(networkTrafficBytesRxLabelsValues, val)
			 networkTrafficBytesTxLabelsValues = append(networkTrafficBytesTxLabelsValues, val)
 
			 networkTrafficPktsLabels = append(networkTrafficPktsLabels, labelPreffix+labelFormatter.Replace(label))
			 networkTrafficPktsRxLabelsValues = append(networkTrafficPktsRxLabelsValues, val)
			 networkTrafficPktsTxLabelsValues = append(networkTrafficPktsTxLabelsValues, val)
 
			 networkErrorsLabels = append(networkErrorsLabels, labelPreffix+labelFormatter.Replace(label))
			 networkErrorsRxLabelsValues = append(networkErrorsRxLabelsValues, val)
			 networkErrorsTxLabelsValues = append(networkErrorsTxLabelsValues, val)
		 }
 
		 // Add k8s metadata.Annotations as metric labels
		 annotationPreffix := "vmi_k8s_annotation_"
		 for annotation, val := range vmi.Annotations {
			 networkTrafficBytesLabels = append(networkTrafficBytesLabels, annotationPreffix+labelFormatter.Replace(annotation))
			 networkTrafficBytesRxLabelsValues = append(networkTrafficBytesRxLabelsValues, val)
			 networkTrafficBytesTxLabelsValues = append(networkTrafficBytesTxLabelsValues, val)
 
			 networkTrafficPktsLabels = append(networkTrafficPktsLabels, annotationPreffix+labelFormatter.Replace(annotation))
			 networkTrafficPktsRxLabelsValues = append(networkTrafficPktsRxLabelsValues, val)
			 networkTrafficPktsTxLabelsValues = append(networkTrafficPktsTxLabelsValues, val)
 
			 networkErrorsLabels = append(networkErrorsLabels, annotationPreffix+labelFormatter.Replace(annotation))
			 networkErrorsRxLabelsValues = append(networkErrorsRxLabelsValues, val)
			 networkErrorsTxLabelsValues = append(networkErrorsTxLabelsValues, val)
		 }
 
		 networkTrafficBytesDesc = prometheus.NewDesc(
			 "kubevirt_vmi_network_traffic_bytes_total",
			 "network traffic.",
			 networkTrafficBytesLabels,
			 nil,
		 )
 
		 networkTrafficPktsDesc = prometheus.NewDesc(
			 "kubevirt_vmi_network_traffic_packets_total",
			 "network traffic.",
			 networkTrafficPktsLabels,
			 nil,
		 )
 
		 networkErrorsDesc = prometheus.NewDesc(
			 "kubevirt_vmi_network_errors_total",
			 "network errors.",
			 networkErrorsLabels,
			 nil,
		 )
 
		 if !net.NameSet {
			 continue
		 }
		 if net.RxBytesSet {
			 mv, err := prometheus.NewConstMetric(
				 networkTrafficBytesDesc, prometheus.CounterValue,
				 float64(net.RxBytes),
				 networkTrafficBytesRxLabelsValues...
			 )
			 tryToPushMetric(networkTrafficBytesDesc, mv, err, ch)
		 }
		 if net.RxPktsSet {
			 mv, err := prometheus.NewConstMetric(
				 networkTrafficPktsDesc, prometheus.CounterValue,
				 float64(net.RxPkts),
				 networkTrafficPktsRxLabelsValues...
			 )
			 tryToPushMetric(networkTrafficPktsDesc, mv, err, ch)
		 }
		 if net.RxErrsSet {
			 mv, err := prometheus.NewConstMetric(
				 networkErrorsDesc, prometheus.CounterValue,
				 float64(net.RxErrs),
				 networkErrorsRxLabelsValues...
			 )
			 tryToPushMetric(networkErrorsDesc, mv, err, ch)
		 }
 
		 if net.TxBytesSet {
			 mv, err := prometheus.NewConstMetric(
				 networkTrafficBytesDesc, prometheus.CounterValue,
				 float64(net.TxBytes),
				 networkTrafficBytesTxLabelsValues...
			 )
			 tryToPushMetric(networkTrafficBytesDesc, mv, err, ch)
		 }
		 if net.TxPktsSet {
			 mv, err := prometheus.NewConstMetric(
				 networkTrafficPktsDesc, prometheus.CounterValue,
				 float64(net.TxPkts),
				 networkTrafficPktsTxLabelsValues...
			 )
			 tryToPushMetric(networkTrafficPktsDesc, mv, err, ch)
		 }
		 if net.TxErrsSet {
			 mv, err := prometheus.NewConstMetric(
				 networkErrorsDesc, prometheus.CounterValue,
				 float64(net.TxErrs),
				 networkErrorsTxLabelsValues...
			 )
			 tryToPushMetric(networkErrorsDesc, mv, err, ch)
		 }
	 }
 }
 
 func makeVMIsPhasesMap(vmis []*k6tv1.VirtualMachineInstance) map[string]uint64 {
	 phasesMap := make(map[string]uint64)
 
	 for _, vmi := range vmis {
		 phasesMap[strings.ToLower(string(vmi.Status.Phase))] += 1
	 }
 
	 return phasesMap
 }
 
 func updateVMIsPhase(nodeName string, vmis []*k6tv1.VirtualMachineInstance, ch chan<- prometheus.Metric) {
	 phasesMap := makeVMIsPhasesMap(vmis)
 
	 for phase, count := range phasesMap {
		 mv, err := prometheus.NewConstMetric(
			 vmiCountDesc, prometheus.GaugeValue,
			 float64(count),
			 nodeName, phase,
		 )
		 if err != nil {
			 continue
		 }
		 ch <- mv
	 }
 }
 
 func updateVersion(ch chan<- prometheus.Metric) {
	 verinfo := version.Get()
	 ch <- prometheus.MustNewConstMetric(
		 versionDesc, prometheus.GaugeValue,
		 1.0,
		 verinfo.GoVersion, verinfo.GitVersion,
	 )
 }
 
 type Collector struct {
	 virtCli       kubecli.KubevirtClient
	 virtShareDir  string
	 nodeName      string
	 concCollector *concurrentCollector
 }
 
 func SetupCollector(virtCli kubecli.KubevirtClient, virtShareDir, nodeName string, MaxRequestsInFlight int) *Collector {
	 log.Log.Infof("Starting collector: node name=%v", nodeName)
	 co := &Collector{
		 virtCli:       virtCli,
		 virtShareDir:  virtShareDir,
		 nodeName:      nodeName,
		 concCollector: NewConcurrentCollector(MaxRequestsInFlight),
	 }
	 prometheus.MustRegister(co)
	 return co
 }
 
 func (co *Collector) Describe(ch chan<- *prometheus.Desc) {
	 // TODO: Use DescribeByCollect?
	 ch <- versionDesc
	 ch <- storageIopsDesc
	 ch <- storageTrafficDesc
	 ch <- storageTimesDesc
	 ch <- vcpuUsageDesc
	 ch <- networkTrafficBytesDesc
	 ch <- networkTrafficPktsDesc
	 ch <- networkErrorsDesc
	 ch <- memoryAvailableDesc
	 ch <- memoryResidentDesc
 }
 
 func newvmiSocketMapFromVMIs(baseDir string, vmis []*k6tv1.VirtualMachineInstance) vmiSocketMap {
	 if len(vmis) == 0 {
		 return nil
	 }
 
	 ret := make(vmiSocketMap)
	 for _, vmi := range vmis {
		 socketPath, err := cmdclient.FindSocketOnHost(vmi)
		 if err != nil {
			 // nothing to scrape...
			 // this means there's no socket or the socket
			 // is currently unreachable for this vmi.
			 continue
		 }
		 ret[socketPath] = vmi
	 }
	 return ret
 }
 
 // Note that Collect could be called concurrently
 func (co *Collector) Collect(ch chan<- prometheus.Metric) {
	 updateVersion(ch)
 
	 vmis, err := lookup.VirtualMachinesOnNode(co.virtCli, co.nodeName)
	 if err != nil {
		 log.Log.Reason(err).Errorf("failed to list all VMIs in '%s': %s", co.nodeName, err)
		 return
	 }
 
	 if len(vmis) == 0 {
		 log.Log.V(4).Infof("No VMIs detected")
		 return
	 }
 
	 socketToVMIs := newvmiSocketMapFromVMIs(co.virtShareDir, vmis)
	 scraper := &prometheusScraper{ch: ch}
	 co.concCollector.Collect(socketToVMIs, scraper, collectionTimeout)
 
	 updateVMIsPhase(co.nodeName, vmis, ch)
	 return
 }
 
 type prometheusScraper struct {
	 ch chan<- prometheus.Metric
 }
 
 type vmiStatsInfo struct {
	 vmiSpec  *k6tv1.VirtualMachineInstance
	 vmiStats *stats.DomainStats
 }
 
 func (ps *prometheusScraper) Scrape(socketFile string, vmi *k6tv1.VirtualMachineInstance) {
	 ts := time.Now()
	 cli, err := cmdclient.NewClient(socketFile)
	 if err != nil {
		 log.Log.Reason(err).Error("failed to connect to cmd client socket")
		 // Ignore failure to connect to client.
		 // These are all local connections via unix socket.
		 // A failure to connect means there's nothing on the other
		 // end listening.
		 return
	 }
	 defer cli.Close()
 
	 vmStats, exists, err := cli.GetDomainStats()
	 if err != nil {
		 log.Log.Reason(err).Errorf("failed to update stats from socket %s", socketFile)
		 return
	 }
	 if !exists || vmStats.Name == "" {
		 log.Log.V(2).Infof("disappearing VM on %s, ignored", socketFile) // VM may be shutting down
		 return
	 }
 
	 // GetDomainStats() may hang for a long time.
	 // If it wakes up past the timeout, there is no point in send back any metric.
	 // In the best case the information is stale, in the worst case the information is stale *and*
	 // the reporting channel is already closed, leading to a possible panic - see below
	 elapsed := time.Now().Sub(ts)
	 if elapsed > statsMaxAge {
		 log.Log.Infof("took too long (%v) to collect stats from %s: ignored", elapsed, socketFile)
		 return
	 }
 
	 ps.Report(socketFile, vmi, vmStats)
 }
 
 func (ps *prometheusScraper) Report(socketFile string, vmi *k6tv1.VirtualMachineInstance, vmStats *stats.DomainStats) {
	 // statsMaxAge is an estimation - and there is not better way to do that. So it is possible that
	 // GetDomainStats() takes enough time to lag behind, but not enough to trigger the statsMaxAge check.
	 // In this case the next functions will end up writing on a closed channel. This will panic.
	 // It is actually OK in this case to abort the goroutine that panicked -that's what we want anyway,
	 // and the very reason we collect in throwaway goroutines. We need however to avoid dump stacktraces in the logs.
	 // Since this is a known failure condition, let's handle it explicitely.
	 defer func() {
		 if err := recover(); err != nil {
			 log.Log.V(2).Warningf("collector goroutine panicked for VM %s: %s", socketFile, err)
		 }
	 }()
 
	 updateMemory(vmi, vmStats, ps.ch)
	 updateVcpu(vmi, vmStats, ps.ch)
	 updateBlock(vmi, vmStats, ps.ch)
	 updateNetwork(vmi, vmStats, ps.ch)
 }
 
 func Handler(MaxRequestsInFlight int) http.Handler {
	 return promhttp.InstrumentMetricHandler(
		 prometheus.DefaultRegisterer,
		 promhttp.HandlerFor(
			 prometheus.DefaultGatherer,
			 promhttp.HandlerOpts{
				 MaxRequestsInFlight: MaxRequestsInFlight,
			 }),
	 )
 }