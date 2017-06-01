// Implementation (work in progress) of the resequencing analysis pipeline used
// to teach the introductory NGS bioinformatics analysis course at SciLifeLab
// as described on this page:
// http://uppnex.se/twiki/do/view/Courses/NgsIntro1502/ResequencingAnalysis.html
// Prerequisites:
// - Samtools
// - BWA
// - Picard
// - GATK
// Install all tools except GATK like this on X/L/K/Ubuntu:
// sudo apt-get install samtools bwa picard-tools
// (GATK needs to be downloaded and installed manually from www.broadinstitute.org/gatk)
package main

import (
	"fmt"

	sp "github.com/scipipe/scipipe"
	"github.com/scipipe/scipipe/components"
)

const (
	fastq_base_url = "http://bioinfo.perdanauniversity.edu.my/tein4ngs/ngspractice/"
	fastq_file_pat = "%s.ILLUMINA.low_coverage.4p_%s.fq"
	ref_base_url   = "http://ftp.ensembl.org/pub/release-75/fasta/homo_sapiens/dna/"
	ref_file       = "Homo_sapiens.GRCh37.75.dna.chromosome.17.fa"
	ref_file_gz    = "Homo_sapiens.GRCh37.75.dna.chromosome.17.fa.gz"
	vcf_base_url   = "http://ftp.1000genomes.ebi.ac.uk/vol1/ftp/phase1/analysis_results/integrated_call_sets/"
	vcf_file       = "ALL.chr17.integrated_phase1_v3.20101123.snps_indels_svs.genotypes.vcf.gz"
)

var (
	individuals = []string{"NA06984", "NA12489"}
	samples     = []string{"1", "2"}
)

func main() {

	// --------------------------------------------------------------------------------
	// Initialize pipeline runner
	// --------------------------------------------------------------------------------

	prun := sp.NewPipelineRunner()
	sink := sp.NewSink()

	// --------------------------------------------------------------------------------
	// Download Reference Genome
	// --------------------------------------------------------------------------------
	downloadRefCmd := "wget -O {o:outfile} " + ref_base_url + ref_file_gz
	downloadRef := prun.NewFromShell("download_ref", downloadRefCmd)
	downloadRef.SetPathStatic("outfile", ref_file_gz)

	// --------------------------------------------------------------------------------
	// Unzip ref file
	// --------------------------------------------------------------------------------
	ungzipRefCmd := "gunzip -c {i:in} > {o:out}"
	ungzipRef := prun.NewFromShell("ugzip_ref", ungzipRefCmd)
	ungzipRef.SetPathReplace("in", "out", ".gz", "")
	ungzipRef.In("in").Connect(downloadRef.Out("outfile"))

	// Create a FanOut so multiple downstream processes can read from the
	// ungzip process
	refFanOut := components.NewFanOut()
	refFanOut.InFile.Connect(ungzipRef.Out("out"))
	prun.AddProcess(refFanOut)

	// --------------------------------------------------------------------------------
	// Index Reference Genome
	// --------------------------------------------------------------------------------
	indexRef := prun.NewFromShell("index_ref", "bwa index -a bwtsw {i:index}; echo done > {o:done}")
	indexRef.SetPathExtend("index", "done", ".indexed")
	indexRef.In("index").Connect(refFanOut.Out("index_ref"))

	indexDoneFanOut := components.NewFanOut()
	indexDoneFanOut.InFile.Connect(indexRef.Out("done"))
	prun.AddProcess(indexDoneFanOut)

	// Create (multi-level) maps where we can gather outports from processes
	// for each for loop iteration and access them in the merge step later
	outPorts := make(map[string]map[string]map[string]*sp.FilePort)
	for _, indv := range individuals {
		outPorts[indv] = make(map[string]map[string]*sp.FilePort)
		for _, smpl := range samples {
			outPorts[indv][smpl] = make(map[string]*sp.FilePort)

			// --------------------------------------------------------------------------------
			// Download FastQ component
			// --------------------------------------------------------------------------------
			file_name := fmt.Sprintf(fastq_file_pat, indv, smpl)
			downloadFastQCmd := "wget -O {o:fastq} " + fastq_base_url + file_name
			downloadFastQ := prun.NewFromShell("download_fastq_"+indv+"_"+smpl, downloadFastQCmd)
			downloadFastQ.SetPathStatic("fastq", file_name)

			fastQFanOut := components.NewFanOut()
			fastQFanOut.InFile.Connect(downloadFastQ.Out("fastq"))
			prun.AddProcess(fastQFanOut)

			// Save outPorts for later use
			outPorts[indv][smpl]["fastq"] = fastQFanOut.Out("merg")

			// --------------------------------------------------------------------------------
			// BWA Align
			// --------------------------------------------------------------------------------
			bwaAlignCmd := "bwa aln {i:ref} {i:fastq} > {o:sai} # {i:idxdone}"
			bwaAlign := prun.NewFromShell("bwa_aln", bwaAlignCmd)
			bwaAlign.SetPathExtend("fastq", "sai", ".sai")
			bwaAlign.In("ref").Connect(refFanOut.Out("bwa_aln_" + indv + "_" + smpl))
			bwaAlign.In("idxdone").Connect(indexDoneFanOut.Out("bwa_aln_" + indv + "_" + smpl))
			bwaAlign.In("fastq").Connect(fastQFanOut.Out("bwa_aln"))

			// Save outPorts for later use
			outPorts[indv][smpl]["sai"] = bwaAlign.Out("sai")
		}

		// --------------------------------------------------------------------------------
		// Merge
		// --------------------------------------------------------------------------------
		// This one is is needed so bwaMergecan take a proper parameter for
		// individual, which it uses to generate output paths
		indParamGen := components.NewStringGenerator(indv)
		prun.AddProcess(indParamGen)

		// bwa sampe process
		bwaMergeCmd := "bwa sampe {i:ref} {i:sai1} {i:sai2} {i:fq1} {i:fq2} > {o:merged} # {i:refdone} {p:indv}"
		bwaMerge := prun.NewFromShell("merge_"+indv, bwaMergeCmd)
		bwaMerge.SetPathCustom("merged", func(t *sp.SciTask) string { return fmt.Sprintf("%s.merged.sam", t.Params["indv"]) })
		bwaMerge.In("ref").Connect(refFanOut.Out("bwa_merge_" + indv))
		bwaMerge.In("refdone").Connect(indexDoneFanOut.Out("bwa_merge_" + indv))
		bwaMerge.In("sai1").Connect(outPorts[indv]["1"]["sai"])
		bwaMerge.In("sai2").Connect(outPorts[indv]["2"]["sai"])
		bwaMerge.In("fq1").Connect(outPorts[indv]["1"]["fastq"])
		bwaMerge.In("fq2").Connect(outPorts[indv]["2"]["fastq"])
		bwaMerge.PP("indv").Connect(indParamGen.Out)

		sink.Connect(bwaMerge.Out("merged"))
	}

	// --------------------------------------------------------------------------------
	// Run pipeline
	// --------------------------------------------------------------------------------

	prun.AddProcess(sink)
	prun.Run()
}
