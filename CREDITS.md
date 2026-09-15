# Credits

## Attribution

dicomtool is a human–AI collaboration. Credit is given by role, reflecting
how the work was actually divided.

### Architecture & Design

Jeffrey Leal <jeffrey.leal@gmail.com>
https://github.com/jeffrey-leal

### Implementation

Claude by Anthropic (https://anthropic.com)
Application code and documentation

## DICOM Standard Reference

Implementation follows the DICOM Standard published by NEMA:

**DICOM PS3 (2024b)**
https://dicom.nema.org/medical/dicom/current

## Open-Source Libraries

| Library | Version | Author / Maintainer | License | Purpose |
|---|---|---|---|---|
| [spf13/cobra](https://github.com/spf13/cobra) | v1.10.2 | spf13 & Cobra contributors | Apache 2.0 | Command-line interface framework |
| [suyashkumar/dicom](https://github.com/suyashkumar/dicom) | v1.1.0 | Suyash Kumar | MIT | DICOM file parsing and data dictionary |

A full list of all transitive dependencies and their versions is recorded in
`go.sum`.

## Project License

dicomtool is released under the MIT License — see the [LICENSE](LICENSE) file.

The open-source libraries listed above are used under their respective licenses
(Apache 2.0 and MIT), each of which permits this use with attribution.
