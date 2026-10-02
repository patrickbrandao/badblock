export function FinalCta() {
  return (
    <>
      <div className="max-w-container mx-auto my-24 md:my-60 flex flex-col items-center text-center w-full py-24 px-8 rounded-xl border border-black/10 dark:border-white/10 bg-black/[0.04] dark:bg-white/[0.04]">
        <p className="text-h1 font-plexSerif font-medium tracking-[-0.025em] mb-3 max-w-[520px] mx-auto">Pronto para começar?</p>
        <p className="text-[18px] mb-6 opacity-60 max-w-[544px] mx-auto">Consulte ASNs, blocos IP, a raiz do DNS e as prestadoras da Anatel pelas APIs abertas do Badblock</p>
        <div className="flex gap-4">
          <a href="/apis" className="relative inline-flex min-w-max px-6 py-3 justify-center items-center font-medium text-white border rounded-lg transition-[background-size,box-shadow] duration-[150ms] ease-in-out [--c3:#381dbd] [--rx:18px] [background-image:radial-gradient(134.26%_244.64%_at_42.92%_-80.36%,var(--c1)_25.45%,var(--c2)_100%)] [background-size:100%_100%] hover:bg-[length:100%_200%] active:scale-95 active:bg-[length:100%_100%] active:shadow-[0px_0px_11.7px_0px_rgba(180,40,180,0.50),0px_0px_28.8px_0px_rgba(102,43,223,0.50)] focus:outline-none focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-pink-600 text-[18px] [--c1:#59497A] [--c2:#59497A] border-[#59497A] hover:shadow-[0px_0px_8px_0px_rgba(89,73,122,0.35),0px_0px_24px_0px_rgba(89,73,122,0.35)] active:[--c1:#4a3d66] active:[--c2:#4a3d66]">Conheça as APIs</a>
          <a href="#" className="relative min-w-max px-6 py-3 justify-center items-center font-medium border border-black/[0.12] dark:border-white/[0.12] rounded-lg transition-colors duration-[150ms] ease-in-out hover:bg-black/5 dark:hover:bg-white/5 focus:outline-none focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-gray-600 text-[18px] hidden md:block whitespace-nowrap">Agendar uma demonstração</a>
        </div>
      </div>
      <div className="flex mx-auto overflow-hidden justify-center items-center">
        <img src="/landing-2/trains/station-floor--light.svg" alt="" loading="lazy" className="h-[210px] block max-w-full max-h-full object-cover dark:hidden" />
        <img src="/landing-2/trains/station-floor--dark.svg" alt="" loading="lazy" className="h-[210px] max-w-full max-h-full object-cover hidden dark:block" />
      </div>
    </>
  );
}
